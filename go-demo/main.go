package main

import (
  "bytes"
  "crypto/hmac"
  "crypto/rand"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "html/template"
  "io"
  "log"
  "net/url"
  "net/http"
  "os"
  "sort"
  "strings"
  "sync"
  "time"
)

type Event struct {
  PaymentID string `json:"payment_id"`
  Reference string `json:"reference"`
  Amount float64 `json:"amount"`
  Status string `json:"status"`
  Timestamp string `json:"timestamp"`
  CardMask string `json:"card_mask,omitempty"`
  ReceivedAt string `json:"received_at"`
}

type Store struct {
  mu sync.Mutex
  events []Event
  seen map[string]bool
}
func newStore() *Store { return &Store{seen: make(map[string]bool)} }
func (s *Store) Add(e Event) bool {
  s.mu.Lock()
  defer s.mu.Unlock()
  key := e.PaymentID + "|" + e.Status
  if s.seen[key] { return false }
  s.seen[key] = true
  s.events = append(s.events, e)
  return true
}
func (s *Store) List() []Event {
  s.mu.Lock()
  defer s.mu.Unlock()
  out := append([]Event{}, s.events...)
  sort.SliceStable(out, func(i,j int) bool { return out[i].ReceivedAt > out[j].ReceivedAt })
  return out
}

type App struct {
  secret string
  gateway string
  publicGateway string
  webhookURL string
  client *http.Client
  store *Store
  demoMu sync.Mutex
  sessions map[string]string
  orders map[string]*DemoOrder
}
func NewApp(secret, gateway, publicGateway, webhookURL string) *App {
  return &App{secret: secret, gateway: strings.TrimRight(gateway, "/"), publicGateway: strings.TrimRight(publicGateway, "/"), webhookURL: webhookURL, client: &http.Client{Timeout: 8*time.Second}, store: newStore(), sessions: make(map[string]string), orders: make(map[string]*DemoOrder)}
}
func (a *App) Routes() http.Handler {
  mux := http.NewServeMux()
  mux.HandleFunc("GET /", a.shop)
  mux.HandleFunc("POST /demo/login", a.demoLogin)
  mux.HandleFunc("POST /demo/pay", a.demoPay)
  mux.HandleFunc("GET /demo/orders", a.demoOrders)
  
  mux.HandleFunc("POST /webhook", a.webhook)
  
  mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]string{"status":"ok"}) })
  return mux
}
func writeJSON(w http.ResponseWriter, code int, v any) {
  w.Header().Set("Content-Type", "application/json; charset=utf-8")
  w.WriteHeader(code)
  _ = json.NewEncoder(w).Encode(v)
}

// AcquireMock signs JSON encoded with Python json.dumps(..., sort_keys=True).
// Default separators include spaces, and escape_non_ascii=true.
// The gateway currently emits ASCII keys/values in the webhook payload.
func pythonCanonical(raw []byte) ([]byte, error) {
  var m map[string]json.RawMessage
  if err := json.Unmarshal(raw, &m); err != nil { return nil, err }
  keys := make([]string,0,len(m))
  for k := range m { keys=append(keys,k) }
  sort.Strings(keys)
  parts := make([]string,0,len(keys))
  for _,k := range keys {
    key,_ := json.Marshal(k)
    var compact bytes.Buffer
    if err := json.Compact(&compact,m[k]); err!=nil { return nil,err }
    parts=append(parts,string(key)+": "+compact.String())
  }
  return []byte("{"+strings.Join(parts,", ")+"}"),nil
}
func signature(data []byte, secret string) (string,error) {
  canon,err:=pythonCanonical(data)
  if err!=nil {return "",err}
  mac:=hmac.New(sha256.New,[]byte(secret)); _,_=mac.Write(canon)
  return hex.EncodeToString(mac.Sum(nil)),nil
}
func (a *App) webhook(w http.ResponseWriter,r *http.Request) {
  r.Body=http.MaxBytesReader(w,r.Body,64<<10)
  raw,err:=io.ReadAll(r.Body)
  if err!=nil {http.Error(w,"invalid payload",http.StatusBadRequest);return}
  expected,err:=signature(raw,a.secret)
  if err!=nil {http.Error(w,"invalid JSON",http.StatusBadRequest);return}
  received:=strings.TrimSpace(r.Header.Get("X-Signature"))
  if !hmac.Equal([]byte(received),[]byte(expected)) {http.Error(w,"invalid HMAC signature",http.StatusForbidden);return}
  var event Event
  if err:=json.Unmarshal(raw,&event);err!=nil || event.PaymentID=="" || event.Reference=="" {log.Printf("Rejected webhook: invalid event: %v",err);http.Error(w,"invalid event",http.StatusBadRequest);return}
  event.ReceivedAt=time.Now().UTC().Format(time.RFC3339)
  a.demoMu.Lock()
  order:=a.orders[event.Reference]
  if order==nil || order.PaymentID!=event.PaymentID || float64(order.Amount)!=event.Amount || event.Status!="paid" { a.demoMu.Unlock(); http.Error(w,"unknown or mismatched payment",400); return }
  order.Status="paid"
  a.demoMu.Unlock()
  added:=a.store.Add(event)
  log.Printf("Webhook accepted: payment=%s status=%s new=%t",event.PaymentID,event.Status,added)
  writeJSON(w,http.StatusOK,map[string]any{"received":true,"new":added})
}
func (a *App) events(w http.ResponseWriter,r *http.Request) {writeJSON(w,http.StatusOK,a.store.List())}

var page=template.Must(template.New("index").Parse(`<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Money • Payment Mock</title>
<style>
:root{font-family:system-ui,-apple-system,Segoe UI,sans-serif;color:#e7e9f6;background:#0c1020}
*{box-sizing:border-box}body{margin:0;padding:30px 18px}.wrap{max-width:950px;margin:auto}
h1{font-size:34px;margin:0 0 8px}.subtitle{color:#a5aac5;margin-bottom:28px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}
.card{background:#171c30;border:1px solid #303750;border-radius:15px;padding:24px}
label{display:block;margin:16px 0 6px;color:#c3c7dd;font-size:14px}
input{width:100%;padding:12px;border:1px solid #4b526c;border-radius:9px;background:#101528;color:white;font-size:16px}
button,.btn{display:inline-block;border:0;border-radius:9px;background:#8d7aff;color:#100a28;padding:12px 20px;font-weight:700;cursor:pointer;text-decoration:none;margin-top:20px}
pre{overflow:auto;white-space:pre-wrap;word-break:break-word;background:#0d1222;padding:15px;border-radius:9px}
.small{color:#a5aac5;font-size:13px}a{color:#b4a8ff}table{width:100%;border-collapse:collapse;font-size:13px}td,th{text-align:left;padding:10px;border-bottom:1px solid #303750;word-break:break-all}
.status{color:#7aeab9}@media(max-width:720px){.grid{grid-template-columns:1fr}}
</style></head><body><div class="wrap"><h1>Money <span style="color:#a897ff">/ Demo</span></h1>
<p class="subtitle">Тестовый магазин на Go → AcquireMock → HMAC webhook → события. Никаких реальных списаний.</p>
<div class="grid"><section class="card"><h2>Создать платёж</h2>
<form id="pay"><label>Номер заказа</label><input name="reference" required value="ORDER-DEMO-001">
<label>Сумма, USD (1500 = $1500.00)</label><input name="amount" type="number" min="1" max="100000000" required value="1500">
<button>Создать счёт</button></form><p class="small" id="message"></p>
<div id="checkout" hidden><a class="btn" id="checkoutLink" target="_blank" rel="noopener">Открыть оплату ↗</a>
<p class="small">Карта: 4444 4444 4444 4444 · срок — будущая дата · CVV — любые 3 цифры. OTP: 1234.</p></div>
</section><section class="card"><h2>Как это работает</h2><ol><li>Создайте счёт в форме</li><li>Перейдите на страницу оплаты</li><li>Введите тестовую карту и почту</li><li>Введите OTP 1234</li><li>Получите подтверждённый webhook ниже</li></ol>
<p class="small">Тестовый OTP: <code>1234</code></p><a href="{{.PublicGateway}}/test" target="_blank" rel="noopener">Встроенный тестовый UI AcquireMock ↗</a></section></div>
<section class="card" style="margin-top:18px"><h2>Полученные webhooks <span class="status" id="count"></span></h2><p class="small">Автообновление каждые 3 секунды, проверка HMAC-SHA256, повторы не дублируются</p>
<div style="overflow-x:auto"><table><thead><tr><th>Заказ</th><th>Payment ID</th><th>Сумма</th><th>Статус</th><th>Получен</th></tr></thead><tbody id="events"><tr><td colspan="5">Пока пусто</td></tr></tbody></table></div></section></div>
<script>
const form=document.getElementById('pay'),message=document.getElementById('message');
form.addEventListener('submit',async e=>{e.preventDefault();message.textContent='Создание счёта...';document.getElementById('checkout').hidden=true;
try{const body=new URLSearchParams(new FormData(form));const res=await fetch('/create',{method:'POST',body});const data=await res.json();if(!res.ok)throw new Error(data.error||'Ошибка создания');
message.textContent='Счёт создан. Откройте страницу оплаты.';document.getElementById('checkoutLink').href=data.pageUrl;document.getElementById('checkout').hidden=false;
}catch(err){message.textContent='Ошибка: '+err.message}});
async function refresh(){try{const res=await fetch('/events');const events=await res.json();document.getElementById('count').textContent='('+events.length+')';
const tbody=document.getElementById('events');tbody.replaceChildren();if(!events.length){const tr=tbody.insertRow();const td=tr.insertCell();td.colSpan=5;td.textContent='Пока пусто';return;}
for(const ev of events){const tr=tbody.insertRow();for(const v of [ev.reference,ev.payment_id,ev.amount.toFixed(2),ev.status,ev.received_at]){tr.insertCell().textContent=v??'';}}}catch(_){}}
refresh();setInterval(refresh,3000);
</script></body></html>`))

func (a *App) index(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Content-Type","text/html; charset=utf-8")
  _=page.Execute(w,struct{PublicGateway string}{a.publicGateway})
}
func (a *App) create(w http.ResponseWriter,r *http.Request) {
  if err:=r.ParseForm();err!=nil {http.Error(w,"invalid form",400);return}
  reference:=strings.TrimSpace(r.FormValue("reference"))
  if len(reference)==0||len(reference)>100 {http.Error(w,"invalid reference",400);return}
  var amount int64
  if _,err:=fmt.Sscan(r.FormValue("amount"),&amount);err!=nil||amount<=0||amount>100000000 {http.Error(w,"invalid amount",400);return}
  a.issueInvoice(w,r,reference,amount)
}
func (a *App) issueInvoice(w http.ResponseWriter,r *http.Request,reference string,amount int64) {
  payload:=map[string]any{"amount":amount,"reference":reference,"webhookUrl":a.webhookURL,"redirectUrl":"http://localhost:8080/"}
  body,_:=json.Marshal(payload)
  req,err:=http.NewRequestWithContext(r.Context(),http.MethodPost,a.gateway+"/api/create-invoice",bytes.NewReader(body))
  if err!=nil {http.Error(w,"invalid gateway",500);return}
  req.Header.Set("Content-Type","application/json")
  resp,err:=a.client.Do(req)
  if err!=nil {writeJSON(w,502,map[string]string{"error":"Payment gateway unavailable"});return}
  defer resp.Body.Close()
  b,err:=io.ReadAll(io.LimitReader(resp.Body,4096))
  if err!=nil||resp.StatusCode!=200 {writeJSON(w,502,map[string]string{"error":"Payment gateway rejected invoice"});return}
  var result struct{PageURL string `json:"pageUrl"`}
  if json.Unmarshal(b,&result)!=nil||result.PageURL=="" {writeJSON(w,502,map[string]string{"error":"Invalid response from gateway"});return}
  // Rebuild only from a validated checkout path, never trust the supplied host.
  parsed,err:=url.Parse(result.PageURL)
  if err!=nil || !strings.HasPrefix(parsed.Path,"/checkout/") || len(strings.TrimPrefix(parsed.Path,"/checkout/"))<8 {writeJSON(w,502,map[string]string{"error":"Unexpected checkout URL from gateway"});return}
  result.PageURL=a.publicGateway+parsed.EscapedPath()
  writeJSON(w,200,map[string]string{"pageUrl":result.PageURL})
}
func env(k, fallback string) string {if v:=os.Getenv(k);v!="" {return v};return fallback}
func main() {
  secret:=env("WEBHOOK_SECRET","")
  if secret=="" {log.Fatal("WEBHOOK_SECRET is required")}
  app:=NewApp(secret,env("GATEWAY_URL","http://payment-mock:8000"),env("PUBLIC_GATEWAY_URL","http://localhost:8000"),env("WEBHOOK_URL","http://go-demo:8080/webhook"))
  addr:=env("ADDR",":8080")
  log.Printf("Go demo listening on %s",addr)
  if err:=http.ListenAndServe(addr,app.Routes());err!=nil&&!errors.Is(err,http.ErrServerClosed){log.Fatal(err)}
}

type DemoOrder struct {
  ID string `json:"id"`
  Owner string `json:"-"`
  Amount int64 `json:"amount"`
  Status string `json:"status"`
  PaymentID string `json:"payment_id"`
  PageURL string `json:"page_url,omitempty"`
}
func (a *App) clientID(r *http.Request) string {
  c,e:=r.Cookie("demo_session"); if e!=nil{return ""}
  a.demoMu.Lock(); defer a.demoMu.Unlock(); return a.sessions[c.Value]
}
func (a *App) demoLogin(w http.ResponseWriter,r *http.Request) {
  if r.ParseForm()!=nil {http.Error(w,"bad form",400);return}
  id:=r.FormValue("client"); if id!="alice"&&id!="bob" {http.Error(w,"unknown client",400);return}
  b:=make([]byte,32); if _,e:=rand.Read(b);e!=nil{http.Error(w,"session error",500);return}
  token:=hex.EncodeToString(b)
  a.demoMu.Lock(); if old,e:=r.Cookie("demo_session");e==nil{delete(a.sessions,old.Value)}; a.sessions[token]=id; a.demoMu.Unlock()
  http.SetCookie(w,&http.Cookie{Name:"demo_session",Value:token,Path:"/",HttpOnly:true,SameSite:http.SameSiteStrictMode})
  http.Redirect(w,r,"/",303)
}
func (a *App) demoOrders(w http.ResponseWriter,r *http.Request) {
  owner:=a.clientID(r); if owner==""{http.Error(w,"login required",401);return}
  a.demoMu.Lock(); defer a.demoMu.Unlock()
  out:=[]DemoOrder{}; for _,o:=range a.orders{if o.Owner==owner{out=append(out,*o)}}
  writeJSON(w,200,out)
}
func (a *App) demoPay(w http.ResponseWriter,r *http.Request) {
  owner:=a.clientID(r); if owner==""{http.Error(w,"login required",401);return}
  if r.ParseForm()!=nil{http.Error(w,"bad form",400);return}
  id:=strings.TrimSpace(r.FormValue("order")); if id==""||len(id)>60{http.Error(w,"invalid order",400);return}
  // Demo catalog: server-owned orders, not client-provided prices.
  prices:=map[string]int64{"1001":1500,"1002":2500,"2001":900,"2002":3200}
  owners:=map[string]string{"1001":"alice","1002":"alice","2001":"bob","2002":"bob"}
  if owners[id]!=owner{http.Error(w,"order not found",404);return}
  a.demoMu.Lock(); defer a.demoMu.Unlock()
  if o:=a.orders[id];o!=nil{if o.Status=="paid"{http.Error(w,"already paid",409);return};http.Redirect(w,r,o.PageURL,303);return}
  payload,_:=json.Marshal(map[string]any{"amount":prices[id],"reference":id,"webhookUrl":a.webhookURL,"redirectUrl":"http://localhost:8080/"})
  req,e:=http.NewRequestWithContext(r.Context(),"POST",a.gateway+"/api/create-invoice",bytes.NewReader(payload));if e!=nil{http.Error(w,"gateway error",502);return}
  req.Header.Set("Content-Type","application/json")
  resp,e:=a.client.Do(req);if e!=nil{http.Error(w,"gateway unavailable",502);return};defer resp.Body.Close()
  var result struct{PageURL string `json:"pageUrl"`}
  if resp.StatusCode!=200||json.NewDecoder(io.LimitReader(resp.Body,4096)).Decode(&result)!=nil{http.Error(w,"gateway error",502);return}
  u,e:=url.Parse(result.PageURL);if e!=nil||!strings.HasPrefix(u.Path,"/checkout/"){http.Error(w,"invalid checkout",502);return}
  pid:=strings.TrimPrefix(u.Path,"/checkout/");if pid==""||strings.Contains(pid,"/"){http.Error(w,"invalid checkout",502);return}
  checkout:=a.publicGateway+u.EscapedPath()
  a.orders[id]=&DemoOrder{ID:id,Owner:owner,Amount:prices[id],Status:"pending",PaymentID:pid,PageURL:checkout}
  http.Redirect(w,r,checkout,303)
}
var shopPage=template.Must(template.New("shop").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Money / Shop</title><style>body{background:#101424;color:#eee;font:16px system-ui;max-width:850px;margin:40px auto;padding:16px}section{background:#1c2336;padding:24px;border-radius:12px;margin:16px 0}input,select,button{padding:12px;margin:8px;border-radius:7px;font:inherit}button{background:#a392ff;border:0;cursor:pointer}table{width:100%;text-align:left}td,th{padding:10px;border-bottom:1px solid #444}small{color:#b9bed1}</style><h1>Money / Магазин</h1><section><h2>Выбрать клиента</h2><form action="/demo/login" method="post"><select name="client"><option value="alice">Алиса</option><option value="bob">Борис</option></select><button>Войти</button></form><small>Демонстрационное переключение личности, не настоящая авторизация.</small></section>{{if .}}<section><h2>Клиент: {{.}}</h2><p>{{if eq . "alice"}}Заказы: 1001 — $1500; 1002 — $2500{{else}}Заказы: 2001 — $900; 2002 — $3200{{end}}</p><form action="/demo/pay" method="post"><input name="order" placeholder="Номер заказа" required><button>Оплатить</button></form><small>Карта 4444 4444 4444 4444, OTP 1234. Сумма определяется сервером.</small></section><section><h2>Мои платежи</h2><table><thead><tr><th>Заказ</th><th>Сумма USD</th><th>Статус</th><th>Payment ID</th></tr></thead><tbody id="rows"></tbody></table><p id="error"></p></section><script>async function refresh(){try{let r=await fetch('/demo/orders');if(!r.ok)throw Error('Ошибка '+r.status);let data=await r.json();let b=document.getElementById('rows');b.replaceChildren();for(let o of data){let tr=b.insertRow();for(let v of [o.id,o.amount,o.status,o.payment_id])tr.insertCell().textContent=v}document.getElementById('error').textContent=''}catch(e){document.getElementById('error').textContent=e.message}}refresh();setInterval(refresh,3000)</script>{{end}}</html>`))
func (a *App) shop(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","text/html; charset=utf-8"); _=shopPage.Execute(w,a.clientID(r))}
