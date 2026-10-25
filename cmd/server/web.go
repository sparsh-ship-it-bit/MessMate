package main

import (
    "bytes"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "database/sql"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/url"
    "errors"
    "net/http"
    "net/smtp"
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
    mux.HandleFunc("POST /api/v1/auth/forgot-password", requestPasswordReset)
    mux.HandleFunc("POST /api/v1/auth/reset-password", completePasswordReset)
    mux.HandleFunc("GET /api/v1/subscriptions/renew-info/{id}", renewalInfo)
    mux.HandleFunc("POST /api/v1/subscriptions/renew", renewSubscription)
    mux.HandleFunc("POST /api/v1/feedback", submitFeedback)
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
    mux.Handle("/", staticCacheHeaders(http.FileServer(http.Dir(dir))))
}

type feedbackRequest struct { Subject string `json:"subject"`; Description string `json:"description"` }

// submitFeedback sends feedback to the configured team inbox through SMTP.
func submitFeedback(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 12*1024)
    var req feedbackRequest
    if decode(r, &req) != nil { errorJSON(w, 400, "invalid feedback request"); return }
    req.Subject = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(req.Subject, "\r", " "), "\n", " "))
    req.Description = strings.TrimSpace(req.Description)
    if req.Subject == "" || len(req.Subject) > 120 || req.Description == "" || len(req.Description) > 5000 {
        errorJSON(w, 400, "subject (1–120 characters) and description (1–5000 characters) are required"); return
    }
    host, port := strings.TrimSpace(os.Getenv("SMTP_HOST")), getenv("SMTP_PORT", "587")
    username, password := strings.TrimSpace(os.Getenv("SMTP_USERNAME")), os.Getenv("SMTP_PASSWORD")
    from, to := strings.TrimSpace(os.Getenv("SMTP_FROM")), strings.TrimSpace(os.Getenv("FEEDBACK_EMAIL"))
    if host == "" || username == "" || password == "" || from == "" || to == "" {
        errorJSON(w, 503, "Feedback email is not configured yet. Please try again later."); return
    }
    if strings.ContainsAny(from+to, "\r\n") || !strings.Contains(from, "@") || !strings.Contains(to, "@") {
        errorJSON(w, 503, "Feedback email configuration is invalid."); return
    }
    message := "From: MessMate Feedback <" + from + ">\r\n" +
        "To: " + to + "\r\n" +
        "Subject: [MessMate Feedback] " + req.Subject + "\r\n" +
        "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" +
        "New feedback submitted from MessMate.\r\n\r\nSubject: " + req.Subject + "\r\n\r\nDescription:\r\n" + req.Description + "\r\n"
    auth := smtp.PlainAuth("", username, password, host)
    if err := smtp.SendMail(host+":"+port, auth, from, []string{to}, []byte(message)); err != nil {
        log.Printf("feedback email send failed: %v", err)
        errorJSON(w, 502, "Could not send feedback right now. Please try again later."); return
    }
    writeJSON(w, 200, map[string]string{"message":"Feedback sent successfully. Thank you!"})
}

func staticCacheHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        switch {
        case strings.HasPrefix(path, "/assets/"):
            // Vite emits content-hashed filenames in /assets, so these are safe to cache.
            w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
        case path == "/" || path == "/index.html":
            // Always revalidate HTML so a deployment's new asset references take effect.
            w.Header().Set("Cache-Control", "no-cache, must-revalidate")
        case strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css"):
            // Non-hashed standalone files can change independently of the HTML.
            w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
        case strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".webp") || strings.HasSuffix(path, ".woff2"):
            w.Header().Set("Cache-Control", "public, max-age=86400, must-revalidate")
        }
        next.ServeHTTP(w, r)
    })
}

type renewalRequest struct {
    ConsumerID string `json:"consumer_id"`
    Period     string `json:"period"`
    Month      string `json:"month"`
    StartDate  string `json:"start_date"`
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
    nextStart := end.AddDate(0, 0, 1)
    writeJSON(w, 200, map[string]any{"monthly_amount":monthly,"end_date":end.Format("2006-01-02"),"next_start_date":nextStart.Format("2006-01-02")})
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
    if req.StartDate == "" && req.Month == "" { errorJSON(w, http.StatusBadRequest, "start_date is required"); return }
    if req.Method == "" { req.Method = "upi" }
    if req.Method != "upi" && req.Method != "cash" && req.Method != "bank" && req.Method != "card" { errorJSON(w, http.StatusBadRequest, "invalid payment method"); return }
    var start time.Time
    if req.StartDate != "" {
        start, err = time.Parse("2006-01-02", req.StartDate)
    } else {
        start, err = time.Parse("2006-01", req.Month)
    }
    if err != nil { errorJSON(w, http.StatusBadRequest, "start_date must use YYYY-MM-DD format"); return }
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

    amount := monthlyAmount
    end := start.AddDate(0, 0, 29)
    if req.Period == "half" {
        amount = monthlyAmount / 2
        end = start.AddDate(0, 0, 14)
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
        "month": start.Format("2006-01"),
        "start_date": start.Format("2006-01-02"),
        "end_date": end.Format("2006-01-02"),
        "monthly_amount": monthlyAmount,
        "amount_paid": amount,
        "payment_status": status,
    })
}

type upiSettingsRequest struct { UPIID string }
type upiQRRequest struct {
    ConsumerID string `json:"consumer_id"`
    Month string `json:"month"`
    Period string `json:"period"`
    Amount float64 `json:"amount"`
}

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

type passwordResetRequest struct { Email string `json:"email"`; Token string `json:"token"`; Password string `json:"password"` }

func requestPasswordReset(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
    var req passwordResetRequest
    if decode(r, &req) != nil || strings.TrimSpace(req.Email) == "" || !strings.Contains(req.Email, "@") {
        errorJSON(w, 400, "a valid email is required")
        return
    }
    generic := map[string]string{"message":"If an account exists for this email, password reset instructions will be sent."}
    // Do not reveal whether an email belongs to an account.
    var ownerID string
    err := resetDBQueryRow(r.Context(), strings.ToLower(strings.TrimSpace(req.Email))).Scan(&ownerID)
    if err == sql.ErrNoRows { writeJSON(w, 200, generic); return }
    if err != nil { log.Printf("password reset lookup failed: %v", err); errorJSON(w, 500, "could not process reset request"); return }
    host, port := strings.TrimSpace(os.Getenv("SMTP_HOST")), getenv("SMTP_PORT", "587")
    username, password := strings.TrimSpace(os.Getenv("SMTP_USERNAME")), os.Getenv("SMTP_PASSWORD")
    from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
    if host == "" || username == "" || password == "" || from == "" {
        errorJSON(w, 503, "Password reset email is not configured yet. Please try again later.")
        return
    }
    raw := make([]byte, 32)
    if _, err := rand.Read(raw); err != nil { errorJSON(w, 500, "could not create reset link"); return }
    token := base64.RawURLEncoding.EncodeToString(raw)
    digest := sha256.Sum256([]byte(token))
    tokenHash := hex.EncodeToString(digest[:])
    // Use the shared application database; only a hash of the bearer token is stored.
    if err := savePasswordResetToken(ownerID, tokenHash, time.Now().Add(30*time.Minute)); err != nil {
        log.Printf("password reset token save failed: %v", err); errorJSON(w, 500, "could not create reset link"); return
    }
    baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
    if baseURL == "" { baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")), "/") }
    if baseURL == "" { baseURL = "https://messmate-zhms.onrender.com" }
    link := baseURL + "/?reset_token=" + url.QueryEscape(token)
    message := "From: MessMate <" + from + ">\r\nTo: " + strings.TrimSpace(req.Email) + "\r\nSubject: Reset your MessMate password\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nWe received a request to reset your MessMate password. This link expires in 30 minutes and can be used once:\r\n\r\n" + link + "\r\n\r\nIf you did not request this, you can ignore this email.\r\n"
    auth := smtp.PlainAuth("", username, password, host)
    if err := smtp.SendMail(host+":"+port, auth, from, []string{strings.TrimSpace(req.Email)}, []byte(message)); err != nil {
        log.Printf("password reset email send failed: %v", err)
        errorJSON(w, 502, "Could not send reset email right now. Please try again later.")
        return
    }
    writeJSON(w, 200, generic)
}

var passwordResetDB *sql.DB

func resetDBQueryRow(ctx context.Context, email string) *sql.Row {
    return passwordResetDB.QueryRowContext(ctx, "SELECT id::text FROM owners WHERE lower(email)=$1", email)
}
func savePasswordResetToken(ownerID, tokenHash string, expires time.Time) error {
    _, err := passwordResetDB.Exec("UPDATE password_reset_tokens SET used_at=NOW() WHERE owner_id=$1 AND used_at IS NULL", ownerID)
    if err != nil { return err }
    _, err = passwordResetDB.Exec("INSERT INTO password_reset_tokens(token_hash,owner_id,expires_at) VALUES($1,$2,$3)", tokenHash, ownerID, expires)
    return err
}
func completePasswordReset(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
    var req passwordResetRequest
    if decode(r, &req) != nil || strings.TrimSpace(req.Token)=="" || len(req.Password)<8 {
        errorJSON(w, 400, "valid reset token and a password of at least 8 characters are required")
        return
    }
    digest := sha256.Sum256([]byte(req.Token))
    tokenHash := hex.EncodeToString(digest[:])
    var ownerID string
    err := passwordResetDB.QueryRow("SELECT owner_id::text FROM password_reset_tokens WHERE token_hash=$1 AND used_at IS NULL AND expires_at>NOW()", tokenHash).Scan(&ownerID)
    if err != nil { errorJSON(w, 400, "This reset link is invalid or expired. Request a new one."); return }
    hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
    if err != nil { errorJSON(w, 500, "could not update password"); return }
    tx, err := passwordResetDB.Begin()
    if err != nil { errorJSON(w, 500, "could not update password"); return }
    defer tx.Rollback()
    res, err := tx.Exec("UPDATE password_reset_tokens SET used_at=NOW() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>NOW()", tokenHash)
    if err != nil { errorJSON(w, 500, "could not update password"); return }
    n, _ := res.RowsAffected()
    if n != 1 { errorJSON(w, 400, "This reset link is invalid or expired. Request a new one."); return }
    if _, err = tx.Exec("UPDATE owners SET password_hash=$1 WHERE id=$2", string(hash), ownerID); err != nil {
        errorJSON(w, 500, "could not update password"); return
    }
    if err = tx.Commit(); err != nil { errorJSON(w, 500, "could not update password"); return }
    writeJSON(w, 200, map[string]string{"message":"Password updated. You can now sign in with your new password."})
}
