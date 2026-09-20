package main

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "database/sql"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/url"
    "errors"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
    _ "github.com/lib/pq"
    "github.com/skip2/go-qrcode"
)

// registerWeb serves the compiled React application from the same origin as the API.
// Renewal is handled here because the standalone renewal UI calls these endpoints directly.
func registerWeb(mux *http.ServeMux) {
    mux.HandleFunc("GET /api/v1/subscriptions/renew-info/{id}", renewalInfo)
    mux.HandleFunc("POST /api/v1/subscriptions/renew", renewSubscription)
    mux.HandleFunc("GET /api/v1/payments/upi-settings", paymentUPISettings)
    mux.HandleFunc("PUT /api/v1/payments/upi-settings", updatePaymentUPISettings)
    mux.HandleFunc("POST /api/v1/payments/upi-qr", paymentUPIQR)
    mux.HandleFunc("GET /api/v1/payments/provider-status", paymentProviderStatus)
    mux.HandleFunc("GET /api/v1/payments/razorpay-account", paymentRazorpayAccount)
    mux.HandleFunc("PUT /api/v1/payments/razorpay-account", updateRazorpayAccount)
    mux.HandleFunc("POST /api/v1/payments/qr", createPaymentQR)
    mux.HandleFunc("GET /api/v1/payments/qr/{id}/status", paymentQRStatus)
    mux.HandleFunc("POST /api/v1/payments/webhook/razorpay", razorpayWebhook)
    dir := os.Getenv("WEB_DIR")
    if dir == "" {
        dir = "web/dist"
    }
    mux.Handle("/", http.FileServer(http.Dir(dir)))
}

type renewalRequest struct {
    ConsumerID string `json:"consumer_id"`
    Period     string `json:"period"`
    Month      string `json:"month"`
    Method     string
    Reference  string
}

func renewalInfo(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r)
    if err != nil { errorJSON(w, http.StatusUnauthorized, err.Error()); return }
    consumerID, err := uuid.Parse(r.PathValue("id"))
    if err != nil { errorJSON(w, http.StatusBadRequest, "invalid consumer id"); return }
    db, err := sql.Open("postgres", getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"))
    if err != nil { errorJSON(w, 500, "database connection failed"); return }
    defer db.Close()
    var end time.Time
    var monthly float64
    err = db.QueryRow(`SELECT s.end_date, COALESCE(NULLIF(s.monthly_amount,0),s.amount) FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.consumer_id=$1 AND c.owner_id=$2 ORDER BY s.end_date DESC LIMIT 1`, consumerID, ownerID).Scan(&end, &monthly)
    if err != nil { errorJSON(w, 404, "consumer subscription not found"); return }
    writeJSON(w, 200, map[string]any{"monthly_amount":monthly,"end_date":end.Format("2006-01-02")})
}

func renewalOwnerID(r *http.Request) (uuid.UUID, error) {
    token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    if token == "" { return uuid.Nil, errors.New("missing bearer token") }
    claims := &OwnerClaims{}
    parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
        if t.Method != jwt.SigningMethodHS256 { return nil, errors.New("invalid signing method") }
        return []byte(getenv("JWT_SECRET", "dev-secret-change-me")), nil
    })
    if err != nil || !parsed.Valid { return uuid.Nil, errors.New("invalid or expired token") }
    id, err := uuid.Parse(claims.OwnerID)
    if err != nil { return uuid.Nil, errors.New("invalid owner id") }
    return id, nil
}

func renewSubscription(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r)
    if err != nil { errorJSON(w, http.StatusUnauthorized, err.Error()); return }
    var req renewalRequest
    if decode(r, &req) != nil || req.ConsumerID == "" { errorJSON(w, http.StatusBadRequest, "consumer_id is required"); return }
    if req.Period != "half" && req.Period != "full" { errorJSON(w, http.StatusBadRequest, "period must be half or full"); return }
    if req.Month == "" { errorJSON(w, http.StatusBadRequest, "month is required"); return }
    if req.Method == "" { req.Method = "upi" }
    if req.Method != "upi" && req.Method != "cash" && req.Method != "bank" && req.Method != "card" { errorJSON(w, http.StatusBadRequest, "invalid payment method"); return }
    monthStart, err := time.Parse("2006-01", req.Month)
    if err != nil { errorJSON(w, http.StatusBadRequest, "month must use YYYY-MM format"); return }
    consumerID, err := uuid.Parse(req.ConsumerID)
    if err != nil { errorJSON(w, http.StatusBadRequest, "invalid consumer_id"); return }

    db, err := sql.Open("postgres", getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"))
    if err != nil { errorJSON(w, 500, "database connection failed"); return }
    defer db.Close()
    if err = db.Ping(); err != nil { errorJSON(w, 500, "database connection failed"); return }

    var monthlyAmount float64
    err = db.QueryRow(`SELECT COALESCE(NULLIF(s.monthly_amount,0),s.amount) FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.consumer_id=$1 AND c.owner_id=$2 ORDER BY s.end_date DESC LIMIT 1`, consumerID, ownerID).Scan(&monthlyAmount)
    if err != nil { errorJSON(w, 404, "consumer subscription not found"); return }
    if monthlyAmount <= 0 { errorJSON(w, 400, "consumer monthly amount is not configured"); return }

    start := monthStart
    end := monthStart.AddDate(0, 1, -1)
    amount := monthlyAmount
    if req.Period == "half" {
        amount = monthlyAmount / 2
        end = monthStart.AddDate(0, 0, 14)
    }

    // Historical backfilling is allowed, but overlapping coverage is rejected so the
    // same consumer cannot accidentally receive two subscriptions for the same days.
    var overlap bool
    err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.consumer_id=$1 AND c.owner_id=$2 AND s.start_date <= $4 AND s.end_date >= $3)`, consumerID, ownerID, start, end).Scan(&overlap)
    if err != nil { errorJSON(w, 500, "could not check existing subscriptions"); return }
    if overlap { errorJSON(w, http.StatusConflict, "a subscription already exists for the selected period"); return }

    tx, err := db.Begin()
    if err != nil { errorJSON(w, 500, "could not start renewal"); return }
    defer tx.Rollback()

    var newID uuid.UUID
    status := paymentStatus(amount, amount)
    err = tx.QueryRow(`INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,monthly_amount,amount_paid,month_label,payment_status) VALUES($1,$2,$3,$4,$5,$4,$6,$7) RETURNING id`, consumerID, start, end, amount, monthlyAmount, start.Format("January 2006"), status).Scan(&newID)
    if err != nil { errorJSON(w, 500, "could not create renewal"); return }
    if _, err = tx.Exec("INSERT INTO payments(consumer_id,subscription_id,amount,method,reference) VALUES($1,$2,$3,$4,$5)", consumerID, newID, amount, req.Method, req.Reference); err != nil { errorJSON(w, 500, "could not record renewal payment"); return }
    if err = tx.Commit(); err != nil { errorJSON(w, 500, "could not save renewal"); return }

    writeJSON(w, http.StatusCreated, map[string]any{
        "subscription_id": newID,
        "consumer_id": consumerID,
        "period": req.Period,
        "month": req.Month,
        "start_date": start.Format("2006-01-02"),
        "end_date": end.Format("2006-01-02"),
        "monthly_amount": monthlyAmount,
        "amount_paid": amount,
        "payment_status": status,
    })
}

type upiSettingsRequest struct { UPIID string }
type upiQRRequest struct { ConsumerID string; Month string; Period string; Amount float64 }

func paymentUPISettings(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w, http.StatusUnauthorized, err.Error()); return }
    db, err := sql.Open("postgres", getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err != nil { errorJSON(w,500,"database connection failed"); return }; defer db.Close()
    var name, upi string; if err=db.QueryRow("SELECT name, COALESCE(upi_id,'') FROM owners WHERE id=$1",ownerID).Scan(&name,&upi); err!=nil { errorJSON(w,500,"could not load payment settings"); return }
    writeJSON(w,200,map[string]any{"name":name,"upi_id":upi})
}

func updatePaymentUPISettings(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w,http.StatusUnauthorized,err.Error()); return }
    var req map[string]any; if decode(r,&req)!=nil { errorJSON(w,400,"invalid request"); return }; upi,_:=req["upi_id"].(string); upi=strings.TrimSpace(upi)
    if upi=="" || !strings.Contains(upi,"@") { errorJSON(w,400,"enter a valid UPI ID"); return }
    db, err := sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err!=nil { errorJSON(w,500,"database connection failed"); return }; defer db.Close()
    if _,err=db.Exec("UPDATE owners SET upi_id=$1 WHERE id=$2",upi,ownerID); err!=nil { errorJSON(w,500,"could not save UPI ID"); return }; writeJSON(w,200,map[string]any{"upi_id":upi})
}

func paymentUPIQR(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w,http.StatusUnauthorized,err.Error()); return }
    var req upiQRRequest; if decode(r,&req)!=nil || req.ConsumerID=="" || req.Month=="" || req.Amount<=0 { errorJSON(w,400,"consumer_id, month and positive amount are required"); return }
    if req.Period!="half" && req.Period!="full" { errorJSON(w,400,"period must be half or full"); return }; if _,err=time.Parse("2006-01",req.Month); err!=nil { errorJSON(w,400,"month must use YYYY-MM format"); return }
    consumerID,err:=uuid.Parse(req.ConsumerID); if err!=nil { errorJSON(w,400,"invalid consumer_id"); return }
    db,err:=sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err!=nil { errorJSON(w,500,"database connection failed"); return }; defer db.Close()
    var ownerName,upi,consumerName string; if err=db.QueryRow("SELECT name,COALESCE(upi_id,'') FROM owners WHERE id=$1",ownerID).Scan(&ownerName,&upi); err!=nil { errorJSON(w,500,"could not load payment account"); return }; if upi=="" { errorJSON(w,400,"payment UPI ID is not configured"); return }
    if err=db.QueryRow("SELECT name FROM consumers WHERE id=$1 AND owner_id=$2",consumerID,ownerID).Scan(&consumerName); err!=nil { errorJSON(w,404,"consumer not found"); return }
    note:="MessMate "+consumerName+" "+req.Month; uri:="upi://pay?pa="+url.QueryEscape(upi)+"&pn="+url.QueryEscape(ownerName)+"&am="+fmt.Sprintf("%.2f",req.Amount)+"&cu=INR&tn="+url.QueryEscape(note)
    png,err:=qrcode.Encode(uri,qrcode.Medium,320); if err!=nil { errorJSON(w,500,"could not generate payment QR"); return }
    writeJSON(w,200,map[string]any{"image_base64":base64.StdEncoding.EncodeToString(png),"upi_uri":uri,"upi_id":upi,"amount":req.Amount,"consumer":consumerName,"month":req.Month,"period":req.Period})
}


func paymentProviderStatus(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w, http.StatusUnauthorized, err.Error()); return }
    configured := getenv("RAZORPAY_KEY_ID","") != "" && getenv("RAZORPAY_KEY_SECRET","") != "" && getenv("RAZORPAY_WEBHOOK_SECRET","") != ""
    var accountID string
    db, err := sql.Open("postgres", getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"))
    if err == nil { defer db.Close(); _ = db.QueryRow("SELECT COALESCE(razorpay_account_id,'') FROM owners WHERE id=$1", ownerID).Scan(&accountID) }
    writeJSON(w, http.StatusOK, map[string]any{"provider":"razorpay","configured":configured,"route_enabled":accountID!="","razorpay_account_id":accountID})
}

func paymentRazorpayAccount(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w, http.StatusUnauthorized, err.Error()); return }
    db, err := sql.Open("postgres", getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err != nil { errorJSON(w,500,"database connection failed"); return }; defer db.Close()
    var accountID string
    if err=db.QueryRow("SELECT COALESCE(razorpay_account_id,'') FROM owners WHERE id=$1",ownerID).Scan(&accountID); err!=nil { errorJSON(w,500,"could not load Razorpay account"); return }
    writeJSON(w,200,map[string]any{"razorpay_account_id":accountID,"route_enabled":accountID!=""})
}

func updateRazorpayAccount(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w,http.StatusUnauthorized,err.Error()); return }
    var req map[string]any
    if decode(r,&req)!=nil { errorJSON(w,400,"invalid request"); return }
    accountID,_:=req["razorpay_account_id"].(string); accountID=strings.TrimSpace(accountID)
    if accountID!="" && !strings.HasPrefix(accountID,"acc_") { errorJSON(w,400,"Razorpay linked account ID must start with acc_"); return }
    db, err := sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err!=nil { errorJSON(w,500,"database connection failed"); return }; defer db.Close()
    if _,err=db.Exec("UPDATE owners SET razorpay_account_id=$1 WHERE id=$2",accountID,ownerID);err!=nil{errorJSON(w,500,"could not save Razorpay linked account");return}
    writeJSON(w,200,map[string]any{"razorpay_account_id":accountID,"route_enabled":accountID!=""})
}
type paymentQRRequestV2 struct {
    ConsumerID string `json:"consumer_id"`
    Month string `json:"month"`
    Period string `json:"period"`
    Amount float64 `json:"amount"`
}
type razorQRResponseV2 struct {
    ID string `json:"id"`
    ImageURL string `json:"image_url"`
    ShortURL string `json:"short_url"`
    Status string `json:"status"`
}

func createPaymentQR(w http.ResponseWriter, r *http.Request) {
    ownerID, err := renewalOwnerID(r); if err != nil { errorJSON(w,401,err.Error()); return }
    if getenv("RAZORPAY_KEY_ID","")=="" || getenv("RAZORPAY_KEY_SECRET","")=="" { errorJSON(w,503,"online payments are not configured yet"); return }
    var req paymentQRRequestV2
    if decode(r,&req)!=nil || req.ConsumerID=="" || req.Month=="" || req.Amount<=0 { errorJSON(w,400,"consumer_id, month and positive amount are required"); return }
    if req.Period!="half" && req.Period!="full" { errorJSON(w,400,"period must be half or full"); return }
    monthStart,err:=time.Parse("2006-01",req.Month); if err!=nil { errorJSON(w,400,"month must use YYYY-MM format"); return }
    consumerID,err:=uuid.Parse(req.ConsumerID); if err!=nil { errorJSON(w,400,"invalid consumer_id"); return }
    db,err:=sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")); if err!=nil {errorJSON(w,500,"database connection failed");return}; defer db.Close()
    if err=db.Ping();err!=nil{errorJSON(w,500,"database connection failed");return}
    var consumerName string
    if err=db.QueryRow("SELECT name FROM consumers WHERE id=$1 AND owner_id=$2",consumerID,ownerID).Scan(&consumerName);err!=nil{errorJSON(w,404,"consumer not found");return}
    var monthly float64
    if err=db.QueryRow("SELECT COALESCE(NULLIF(s.monthly_amount,0),s.amount) FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.consumer_id=$1 AND c.owner_id=$2 ORDER BY s.end_date DESC LIMIT 1",consumerID,ownerID).Scan(&monthly);err!=nil{errorJSON(w,404,"consumer subscription not found");return}
    expected:=monthly; if req.Period=="half"{expected=monthly/2}
    if expected<=0 || absFloat(expected-req.Amount)>0.01 {errorJSON(w,400,"payment amount does not match the consumer plan");return}
    start:=monthStart; end:=monthStart.AddDate(0,1,-1); if req.Period=="half"{end=monthStart.AddDate(0,0,14)}
    var overlap bool
    if err=db.QueryRow("SELECT EXISTS(SELECT 1 FROM subscriptions WHERE consumer_id=$1 AND start_date <= $3 AND end_date >= $2)",consumerID,start,end).Scan(&overlap);err!=nil{errorJSON(w,500,"could not check subscription period");return}
    if overlap{errorJSON(w,409,"a subscription already exists for the selected period");return}
    intentID:=uuid.New()
    if _,err=db.Exec("INSERT INTO payment_intents(id,owner_id,consumer_id,month_label,period,amount,status) VALUES($1,$2,$3,$4,$5,$6,'pending')",intentID,ownerID,consumerID,req.Month,req.Period,req.Amount);err!=nil{errorJSON(w,500,"could not create payment request");return}
    var razorpayAccountID string
    if err=db.QueryRow("SELECT COALESCE(razorpay_account_id,'') FROM owners WHERE id=$1",ownerID).Scan(&razorpayAccountID);err!=nil{errorJSON(w,500,"could not load Razorpay account");return}
    if razorpayAccountID==""{errorJSON(w,400,"Razorpay Route linked account is not configured for this mess owner");return}
    payload:=map[string]any{"type":"upi_qr","name":"MessMate - "+consumerName,"usage":"single_use","fixed_amount":true,"payment_amount":int64(req.Amount*100),"description":"MessMate "+consumerName+" "+req.Month,"close_by":time.Now().Add(2*time.Hour).Unix(),"notes":map[string]string{"messmate_intent_id":intentID.String(),"owner_id":ownerID.String(),"consumer_id":consumerID.String(),"month":req.Month,"period":req.Period}}
    raw,_:=json.Marshal(payload)
    response,err:=razorpayAPI("POST","/v1/payments/qr_codes",raw)
    if err!=nil{_,_=db.Exec("UPDATE payment_intents SET status='failed' WHERE id=$1",intentID);errorJSON(w,502,err.Error());return}
    var qr razorQRResponseV2
    if err=json.Unmarshal(response,&qr);err!=nil{errorJSON(w,502,"invalid payment provider response");return}
    if _,err=db.Exec("UPDATE payment_intents SET provider_qr_id=$2 WHERE id=$1",intentID,qr.ID);err!=nil{errorJSON(w,500,"could not save payment request");return}
    writeJSON(w,200,map[string]any{"intent_id":intentID,"qr_id":qr.ID,"image_url":qr.ImageURL,"short_url":qr.ShortURL,"status":"pending","consumer":consumerName,"amount":req.Amount,"month":req.Month,"period":req.Period})
}

func paymentQRStatus(w http.ResponseWriter,r *http.Request){
    ownerID,err:=renewalOwnerID(r);if err!=nil{errorJSON(w,401,err.Error());return}
    intentID,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid payment request");return}
    db,err:=sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"));if err!=nil{errorJSON(w,500,"database connection failed");return};defer db.Close()
    var status string;var paidAt sql.NullTime
    if err=db.QueryRow("SELECT status,paid_at FROM payment_intents WHERE id=$1 AND owner_id=$2",intentID,ownerID).Scan(&status,&paidAt);err!=nil{errorJSON(w,404,"payment request not found");return}
    writeJSON(w,200,map[string]any{"status":status,"paid_at":paidAt.Time})
}

func razorpayAPI(method,path string,body []byte)([]byte,error){
    req,err:=http.NewRequest(method,"https://api.razorpay.com"+path,bytes.NewReader(body));if err!=nil{return nil,err}
    req.Header.Set("Content-Type","application/json");req.SetBasicAuth(getenv("RAZORPAY_KEY_ID",""),getenv("RAZORPAY_KEY_SECRET",""))
    resp,err:=http.DefaultClient.Do(req);if err!=nil{return nil,err};defer resp.Body.Close()
    data,_:=io.ReadAll(resp.Body);if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("payment provider returned HTTP %d",resp.StatusCode)};return data,nil
}

func razorpayWebhook(w http.ResponseWriter,r *http.Request){
    secret:=getenv("RAZORPAY_WEBHOOK_SECRET","");if secret==""{http.Error(w,"webhook not configured",503);return}
    body,err:=io.ReadAll(r.Body);if err!=nil{http.Error(w,"bad request",400);return}
    signature:=r.Header.Get("X-Razorpay-Signature");mac:=hmac.New(sha256.New,[]byte(secret));_,_=mac.Write(body)
    expected:=hex.EncodeToString(mac.Sum(nil));if !hmac.Equal([]byte(expected),[]byte(signature)){http.Error(w,"invalid signature",401);return}
    var event struct{
        Event string `json:"event"`
        Payload struct{
            Payment struct{Entity struct{ID string `json:"id"`;Amount int64 `json:"amount"`;Status string `json:"status"`} `json:"entity"`} `json:"payment"`
            QR struct{Entity struct{ID string `json:"id"`;Notes map[string]any `json:"notes"`} `json:"entity"`} `json:"qr_code"`
        } `json:"payload"`
    }
    if json.Unmarshal(body,&event)!=nil{http.Error(w,"invalid payload",400);return}
    if event.Event!="qr_code.credited"{writeJSON(w,200,map[string]any{"received":true});return}
    rawIntent,_:=event.Payload.QR.Entity.Notes["messmate_intent_id"].(string);intentID,err:=uuid.Parse(rawIntent);if err!=nil{http.Error(w,"missing payment intent",400);return}
    if err=fulfillPaymentIntentV2(intentID,event.Payload.Payment.Entity.ID,event.Payload.Payment.Entity.Amount,event.Payload.Payment.Entity.Status,event.Payload.QR.Entity.ID);err!=nil{http.Error(w,err.Error(),500);return}
    writeJSON(w,200,map[string]any{"received":true})
}

func fulfillPaymentIntentV2(intentID uuid.UUID,providerPayment string,providerAmount int64,providerStatus,qrID string)error{
    if providerStatus!=""&&providerStatus!="captured"{return nil}
    db,err:=sql.Open("postgres",getenv("DATABASE_URL","postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"));if err!=nil{return err};defer db.Close()
    if err=db.Ping();err!=nil{return err}
    tx,err:=db.Begin();if err!=nil{return err};defer tx.Rollback()
    var consumerID uuid.UUID;var month,period,status string;var amount float64
    if err=tx.QueryRow("SELECT consumer_id,month_label,period,amount,status FROM payment_intents WHERE id=$1 FOR UPDATE",intentID).Scan(&consumerID,&month,&period,&amount,&status);err!=nil{return err}
    if status!="pending"{return nil}
    if absFloat(amount-float64(providerAmount)/100)>0.01{return errors.New("payment amount mismatch")}
    var duplicate bool;if err=tx.QueryRow("SELECT EXISTS(SELECT 1 FROM payments WHERE method='razorpay' AND reference=$1)",providerPayment).Scan(&duplicate);err!=nil{return err};if duplicate{return nil}
    ms,err:=time.Parse("2006-01",month);if err!=nil{return err};start:=ms;end:=ms.AddDate(0,1,-1);if period=="half"{end=ms.AddDate(0,0,14)}
    var overlap bool;if err=tx.QueryRow("SELECT EXISTS(SELECT 1 FROM subscriptions WHERE consumer_id=$1 AND start_date <= $3 AND end_date >= $2)",consumerID,start,end).Scan(&overlap);err!=nil{return err}
    if overlap{_,err=tx.Exec("UPDATE payment_intents SET status='failed' WHERE id=$1",intentID);return err}
    var monthly float64;if err=tx.QueryRow("SELECT COALESCE(NULLIF(monthly_amount,0),amount) FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1",consumerID).Scan(&monthly);err!=nil{return err}
    var subID uuid.UUID;if err=tx.QueryRow("INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,monthly_amount,amount_paid,month_label,payment_status) VALUES($1,$2,$3,$4,$5,$4,$6,'paid') RETURNING id",consumerID,start,end,amount,monthly,ms.Format("January 2006")).Scan(&subID);err!=nil{return err}
    if _,err=tx.Exec("INSERT INTO payments(consumer_id,subscription_id,amount,method,reference) VALUES($1,$2,$3,'razorpay',$4)",consumerID,subID,amount,providerPayment);err!=nil{return err}
    var ownerID uuid.UUID; var razorpayAccountID string
    if err=tx.QueryRow("SELECT owner_id FROM payment_intents WHERE id=$1",intentID).Scan(&ownerID);err!=nil{return err}
    if err=tx.QueryRow("SELECT COALESCE(razorpay_account_id,'') FROM owners WHERE id=$1",ownerID).Scan(&razorpayAccountID);err!=nil{return err}
    if razorpayAccountID==""{return errors.New("Razorpay Route linked account is not configured")}
    transferPayload:=map[string]any{"transfers":[]any{map[string]any{"account":razorpayAccountID,"amount":providerAmount,"currency":"INR","notes":map[string]string{"messmate_intent_id":intentID.String(),"consumer_id":consumerID.String()},"linked_account_notes":[]string{"messmate_intent_id","consumer_id"},"on_hold":false}}}
    transferRaw,_:=json.Marshal(transferPayload)
    if _,err=razorpayAPI("POST","/v1/payments/"+providerPayment+"/transfers",transferRaw);err!=nil{return err}
    if _,err=tx.Exec("UPDATE payment_intents SET status='paid',provider_payment_id=$2,provider_qr_id=COALESCE(provider_qr_id,$3),paid_at=NOW() WHERE id=$1",intentID,providerPayment,qrID);err!=nil{return err}
    return tx.Commit()
}

func absFloat(v float64)float64{if v<0{return -v};return v}