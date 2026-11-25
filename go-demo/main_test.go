package main

import (
  "bytes"
  "crypto/hmac"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "net/url"
  "strings"
  "testing"
)

const sample = `{"payment_id":"pay_123","reference":"ORDER-1","amount":1500,"status":"paid","timestamp":"2026-11-25T10:30:00","card_mask":"**** 4444"}`

func TestPythonHMACCanonicalization(t *testing.T) {
  canonical,err:=pythonCanonical([]byte(sample))
  if err!=nil {t.Fatal(err)}
  want:=`{"amount": 1500, "card_mask": "**** 4444", "payment_id": "pay_123", "reference": "ORDER-1", "status": "paid", "timestamp": "2026-11-25T10:30:00"}`
  if string(canonical)!=want {t.Fatalf("canonical JSON mismatch\nwant %s\n got %s",want,canonical)}
  h:=hmac.New(sha256.New,[]byte("secret")); h.Write([]byte(want))
  wantSig:=hex.EncodeToString(h.Sum(nil))
  got,err:=signature([]byte(sample),"secret")
  if err!=nil||got!=wantSig {t.Fatalf("signature mismatch: %q %v",got,err)}
}

func TestWebhookValidAndDuplicate(t *testing.T) {
  app:=NewApp("secret","http://localhost:1","http://localhost:8000","http://localhost:8080/webhook")
  sig,_:=signature([]byte(sample),"secret")
  for i:=0;i<2;i++ {
    req:=httptest.NewRequest(http.MethodPost,"/webhook",strings.NewReader(sample))
    req.Header.Set("X-Signature",sig)
    w:=httptest.NewRecorder()
    app.Routes().ServeHTTP(w,req)
    if w.Code!=200 {t.Fatalf("webhook got %d: %s",w.Code,w.Body.String())}
  }
  if n:=len(app.store.List());n!=1 {t.Fatalf("want one event, got %d",n)}
  w:=httptest.NewRecorder()
  app.Routes().ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/events",nil))
  if w.Code!=200||!strings.Contains(w.Body.String(),"ORDER-1") {t.Fatal("events endpoint failed")}
}
func TestWebhookRejectsTampering(t *testing.T) {
  app:=NewApp("secret","http://localhost:1","http://localhost:8000","http://localhost:8080/webhook")
  sig,_:=signature([]byte(sample),"secret")
  tampered:=strings.Replace(sample,"1500","9000",1)
  req:=httptest.NewRequest("POST","/webhook",strings.NewReader(tampered))
  req.Header.Set("X-Signature",sig)
  w:=httptest.NewRecorder()
  app.Routes().ServeHTTP(w,req)
  if w.Code!=403 {t.Fatalf("want forbidden, got %d",w.Code)}
  if len(app.store.List())!=0 {t.Fatal("tampered event was stored")}
}
func TestCreateInvoice(t *testing.T) {
  gateway:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    if r.URL.Path!="/api/create-invoice"||r.Method!="POST" {t.Fatal("wrong route")}
    var body map[string]any
    if err:=json.NewDecoder(r.Body).Decode(&body);err!=nil {t.Fatal(err)}
    if body["webhookUrl"]!="http://go-demo:8080/webhook" || body["amount"]!=float64(1500) {t.Fatalf("unexpected body: %+v",body)}
    w.Header().Set("Content-Type","application/json")
    w.Write([]byte(`{"pageUrl":"http://payment-mock:8000/checkout/12345678-abcd"}`))
  }))
  defer gateway.Close()
  app:=NewApp("secret",gateway.URL,"http://localhost:8000","http://go-demo:8080/webhook")
  form:=url.Values{"reference":{"DEMO-1"},"amount":{"1500"}}
  w:=httptest.NewRecorder()
  req:=httptest.NewRequest("POST","/create",bytes.NewBufferString(form.Encode()))
  req.Header.Set("Content-Type","application/x-www-form-urlencoded")
  app.Routes().ServeHTTP(w,req)
  if w.Code!=200||!strings.Contains(w.Body.String(),"http://localhost:8000/checkout/12345678-abcd") {t.Fatalf("create failed %d %s",w.Code,w.Body.String())}
}
func TestCreateRejectsInvalidAmount(t *testing.T) {
  app:=NewApp("secret","http://localhost:1","http://localhost:8000","http://go-demo:8080/webhook")
  form:=url.Values{"reference":{"DEMO-1"},"amount":{"-100"}}
  req:=httptest.NewRequest("POST","/create",strings.NewReader(form.Encode()))
  req.Header.Set("Content-Type","application/x-www-form-urlencoded")
  w:=httptest.NewRecorder()
  app.Routes().ServeHTTP(w,req)
  if w.Code!=400 {t.Fatalf("want bad request, got %d",w.Code)}
}
func TestIndexAndHealth(t *testing.T) {
  app:=NewApp("secret","http://localhost:1","http://localhost:8000","http://go-demo:8080/webhook")
  for _,tc:=range []struct{path,needle string}{{"/","Создать платёж"},{"/health","ok"}} {
    w:=httptest.NewRecorder()
    app.Routes().ServeHTTP(w,httptest.NewRequest("GET",tc.path,nil))
    if w.Code!=200||!strings.Contains(w.Body.String(),tc.needle) {t.Fatalf("%s failed: %d",tc.path,w.Code)}
  }
}
