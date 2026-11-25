package main

import (
  "bytes"
  "crypto/hmac"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "html/template"
  "io"
  "log"
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
  Amount int64 `json:"amount"`
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
  out := append([]Event(nil), s.events...)
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
}
func NewApp(secret, gateway, publicGateway, webhookURL string) *App {
  return &App{secret: secret, gateway: strings.TrimRight(gateway, "/"), publicGateway: strings.TrimRight(publicGateway, "/"), webhookURL: webhookURL, client: &http.Client{Timeout: 8*time.Second}, store: newStore()}
}
func (a *App) Routes() http.Handler {
  mux := http.NewServeMux()
  mux.HandleFunc("GET /", a.index)
  mux.HandleFunc("POST /create", a.create)
  mux.HandleFunc("POST /webhook", a.webhook)
  mux.HandleFunc("GET /events", a.events)
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
func pythonCanonical(value any) ([]byte, error) {
  var m map[string]json.RawMessage
  if err := json.Unmarshal(value.([]byte), &m); err != nil { return nil, err }
  keys := make([]string,0,len(m))
  for k := range m { keys=append(keys,k) }
  sort.Strings(keys)
  parts := make([]string,0,len(keys))
  for _,k := range keys {
    key,_ := json.Marshal(k)
    var compact bytes.Buffer
    if err := json.Compact(&compact,m[k]); err!=nil { return nil,err }
    parts=append(parts,string(key)+": "+strings.ReplaceAll(compact.String(), ",", ", "))
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
  if err:=json.Unmarshal(raw,&event);err!=nil || event.PaymentID=="" || event.Reference=="" {http.Error(w,"invalid event",http.StatusBadRequest);return}
  event.ReceivedAt=time.Now().UTC().Format(time.RFC3339)
  added:=a.store.Add(event)
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
<label>Сумма, центы (1500 = $15.00)</label><input name="amount" type="number" min="1" max="100000000" required value="1500">
<button>Создать счёт</button></form><p class="small" id="message"></p>
<div id="checkout" hidden><a class="btn" id="checkoutLink" target="_blank" rel="noopener">Открыть оплату ↗</a>
<p class="small">Карта: 4444 4444 4444 4444 · срок — будущая дата · CVV — любые 3 цифры. OTP ищите в логах Docker.</p></div>
</section><section class="card"><h2>Как это работает</h2><ol><li>Создайте счёт в форме</li><li>Перейдите на страницу оплаты</li><li>Введите тестовую карту и почту</li><li>Введите OTP из логов payment-mock</li><li>Получите подтверждённый webhook ниже</li></ol>
<p class="small">Для OTP: <code>docker compose logs -f payment-mock</code></p><a href="{{.PublicGateway}}/test" target="_blank" rel="noopener">Встроенный тестовый UI AcquireMock ↗</a></section></div>
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
for(const ev of events){const tr=tbody.insertRow();for(const v of [ev.reference,ev.payment_id,(ev.amount/100).toFixed(2),ev.status,ev.received_at]){tr.insertCell().textContent=v??'';}}}catch(_){}}
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
  // Ensure checkout URL points to the browser-facing address, not a Docker hostname.
  if !strings.HasPrefix(result.PageURL,a.publicGateway+"/") {result.PageURL=a.publicGateway+"/checkout/"+strings.TrimPrefix(result.PageURL,strings.TrimSuffix(result.PageURL,"/"))}
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
