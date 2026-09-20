package main

import (
    "database/sql"
    "encoding/base64"
    "fmt"
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
