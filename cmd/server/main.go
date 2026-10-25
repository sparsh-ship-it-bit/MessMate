package main

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
    _ "github.com/lib/pq"
    "github.com/skip2/go-qrcode"
    "golang.org/x/crypto/bcrypt"
)

type key string
const ownerKey key = "owner_id"
type App struct { db *sql.DB; jwtSecret []byte; ttl time.Duration }
type OwnerClaims struct { OwnerID string `json:"owner_id"`; jwt.RegisteredClaims }
type authRequest struct { Name string `json:"name"`; Email string `json:"email"`; Phone string `json:"phone"`; Password string `json:"password"` }
type loginRequest struct { Email string `json:"email"`; Password string `json:"password"` }
type consumerRequest struct { Name string `json:"name"`; Phone string `json:"phone"`; ConsumerID string `json:"consumer_id"`; MealPlan string `json:"meal_plan"`; StartDate string `json:"start_date"`; EndDate string `json:"end_date"`; Amount float64 `json:"amount"`; AmountPaid float64 `json:"amount_paid"` }
type subscriptionRequest struct { ConsumerID string `json:"consumer_id"`; StartDate string `json:"start_date"`; EndDate string `json:"end_date"`; Amount float64 `json:"amount"`; AmountPaid float64 `json:"amount_paid"`; MonthLabel string `json:"month_label"`; PaymentStatus string `json:"payment_status"` }
type paymentRequest struct { ConsumerID string `json:"consumer_id"`; SubscriptionID string `json:"subscription_id"`; Amount float64 `json:"amount"`; Method string `json:"method"`; Reference string `json:"reference"`; PaidAt string `json:"paid_at"` }
type attendanceRequest struct { QRToken string `json:"qr_token"`; Meal string `json:"meal"` }

func main() {
    loadDotEnv()
    jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
    if len(jwtSecret) < 32 || jwtSecret == "dev-secret-change-me" || jwtSecret == "change-this-in-production" || strings.Contains(jwtSecret, "replace-with-a-strong-random-secret") {
        log.Fatal("JWT_SECRET must be set to a unique secret of at least 32 characters")
    }

    dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")
    db, err := sql.Open("postgres", dsn); if err != nil { log.Fatal(err) }
    // Bound concurrent and idle connections to keep the app predictable on hosted Postgres.
    db.SetMaxOpenConns(10)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)
    if err = db.Ping(); err != nil { log.Fatal("database: ", err) }
    if err = runMigrations(db); err != nil { log.Fatal("migrations: ", err) }
    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS password_reset_tokens (token_hash TEXT PRIMARY KEY, owner_id UUID NOT NULL REFERENCES owners(id) ON DELETE CASCADE, expires_at TIMESTAMPTZ NOT NULL, used_at TIMESTAMPTZ NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil { log.Fatal("password reset migration: ", err) }
    if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS password_reset_tokens_owner_idx ON password_reset_tokens(owner_id)`); err != nil { log.Fatal("password reset index: ", err) }
    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS workers (id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), owner_id UUID NOT NULL REFERENCES owners(id) ON DELETE CASCADE, name VARCHAR(120) NOT NULL, date_joined DATE NOT NULL, shift VARCHAR(40) NOT NULL, qr_token UUID NOT NULL UNIQUE DEFAULT uuid_generate_v4(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil { log.Fatal("workers migration: ", err) }
    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS worker_attendance (id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), worker_id UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE, attendance_date DATE NOT NULL DEFAULT ((NOW() AT TIME ZONE 'Asia/Kolkata')::date), shift VARCHAR(40) NOT NULL, scanned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), out_time TIMESTAMPTZ NULL, UNIQUE(worker_id, attendance_date))`); err != nil { log.Fatal("worker attendance migration: ", err) }
    if _, err = db.Exec(`ALTER TABLE worker_attendance ADD COLUMN IF NOT EXISTS out_time TIMESTAMPTZ NULL`); err != nil { log.Fatal("worker attendance out-time migration: ", err) }
    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS worker_attendance_sessions (id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), worker_id UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE, attendance_date DATE NOT NULL DEFAULT ((NOW() AT TIME ZONE 'Asia/Kolkata')::date), shift VARCHAR(40) NOT NULL, in_time TIMESTAMPTZ NOT NULL DEFAULT NOW(), out_time TIMESTAMPTZ NULL)`); err != nil { log.Fatal("worker attendance sessions migration: ", err) }
    if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS worker_attendance_sessions_worker_date_idx ON worker_attendance_sessions(worker_id, attendance_date, in_time)`); err != nil { log.Fatal("worker attendance sessions index: ", err) }
    if _, err = db.Exec(`INSERT INTO worker_attendance_sessions(worker_id,attendance_date,shift,in_time,out_time) SELECT worker_id,attendance_date,shift,scanned_at,out_time FROM worker_attendance old WHERE NOT EXISTS (SELECT 1 FROM worker_attendance_sessions s WHERE s.worker_id=old.worker_id AND s.attendance_date=old.attendance_date)`); err != nil { log.Fatal("worker attendance legacy migration: ", err) }
    ttlHours, _ := strconv.Atoi(getenv("JWT_TTL_HOURS", "24"))
    app := &App{db: db, jwtSecret: []byte(jwtSecret), ttl: time.Duration(ttlHours)*time.Hour}
    mux := http.NewServeMux()
    mux.HandleFunc("GET /health", app.health)
    mux.HandleFunc("POST /api/v1/auth/register", app.register)
    mux.HandleFunc("POST /api/v1/auth/login", app.login)
    mux.HandleFunc("GET /api/v1/dashboard", app.auth(app.dashboard))
    mux.HandleFunc("GET /api/v1/analytics", app.auth(app.monthlyAnalytics))
    mux.HandleFunc("GET /api/v1/workers", app.auth(app.listWorkers))
    mux.HandleFunc("GET /api/v1/workers/attendance/today", app.auth(app.todayWorkerAttendance))
    mux.HandleFunc("GET /api/v1/workers/{id}/attendance", app.auth(app.workerAttendanceHistory))
    mux.HandleFunc("POST /api/v1/workers/attendance/scan", app.auth(app.scanWorkerAttendance))
    mux.HandleFunc("POST /api/v1/workers", app.auth(app.createWorker))
    mux.HandleFunc("GET /api/v1/workers/{id}/qr", app.auth(app.workerQR))
    mux.HandleFunc("DELETE /api/v1/workers/{id}", app.auth(app.deleteWorker))
    mux.HandleFunc("GET /api/v1/consumers", app.auth(app.listConsumers))
    mux.HandleFunc("POST /api/v1/consumers", app.auth(app.createConsumer))
    mux.HandleFunc("GET /api/v1/consumers/{id}", app.auth(app.getConsumer))
    mux.HandleFunc("GET /api/v1/consumers/{id}/profile", app.auth(app.consumerProfile))
    mux.HandleFunc("PATCH /api/v1/consumers/{id}", app.auth(app.updateConsumer))
    mux.HandleFunc("DELETE /api/v1/consumers/{id}", app.auth(app.deleteConsumer))
    mux.HandleFunc("GET /api/v1/consumers/{id}/qr", app.auth(app.consumerQR))
    mux.HandleFunc("POST /api/v1/subscriptions", app.auth(app.createSubscription))
    mux.HandleFunc("GET /api/v1/subscriptions/expiring-today", app.auth(app.expiringToday))
    mux.HandleFunc("GET /api/v1/payments/pending", app.auth(app.pendingPayments))
    mux.HandleFunc("GET /api/v1/payments/recent", app.auth(app.recentPayments))
    mux.HandleFunc("POST /api/v1/payments", app.auth(app.createPayment))
    mux.HandleFunc("POST /api/v1/mobile/pairing", app.auth(app.createMobilePairing))
    mux.HandleFunc("POST /api/v1/mobile/pair", app.exchangeMobilePairing)
    mux.HandleFunc("POST /api/v1/mobile/scan", app.mobileScan)
    mux.HandleFunc("POST /api/v1/mobile/worker-scan", app.mobileWorkerScan)
    mux.HandleFunc("GET /api/v1/mobile/worker-attendance/today", app.mobileTodayWorkerAttendance)
    mux.HandleFunc("GET /api/v1/mobile/workers", app.mobileListWorkers)
    mux.HandleFunc("GET /api/v1/mobile/workers/{id}/attendance", app.mobileWorkerAttendanceHistory)
    mux.HandleFunc("POST /api/v1/mobile/heartbeat", app.mobileHeartbeat)
    mux.HandleFunc("GET /api/v1/mobile/scans", app.auth(app.mobileScanEvents))
    mux.HandleFunc("POST /api/v1/attendance/scan", app.auth(app.scanAttendance))
    mux.HandleFunc("GET /api/v1/attendance/today", app.auth(app.todayAttendance))
    registerWeb(mux)
    handler := cors(logging(mux))
    port := getenv("PORT", "8080")
    log.Printf("MessMate listening on :%s", port)
    server := &http.Server{
        Addr:              ":" + port,
        Handler:           handler,
        ReadHeaderTimeout: 5 * time.Second,
        IdleTimeout:       60 * time.Second,
    }
    log.Fatal(server.ListenAndServe())
}

func loadDotEnv() {
    data, err := os.ReadFile(".env")
    if err != nil {
        return // Production deployments provide environment variables directly.
    }
    for _, line := range strings.Split(string(data), "\n") {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        key, value, ok := strings.Cut(line, "=")
        if !ok {
            continue
        }
        key = strings.TrimSpace(key)
        value = strings.Trim(strings.TrimSpace(value), "\"'")
        if key == "" {
            continue
        }
        if _, exists := os.LookupEnv(key); !exists {
            _ = os.Setenv(key, value)
        }
    }
}

func getenv(k, fallback string) string { if v:=os.Getenv(k); v!="" { return v }; return fallback }
func writeJSON(w http.ResponseWriter, status int, v any) { w.Header().Set("Content-Type","application/json"); w.WriteHeader(status); _=json.NewEncoder(w).Encode(v) }
func errorJSON(w http.ResponseWriter, status int, msg string) { writeJSON(w,status,map[string]string{"error":msg}) }
func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
func (a *App) health(w http.ResponseWriter, r *http.Request) { writeJSON(w,200,map[string]string{"status":"ok","service":"messmate"}) }

func (a *App) register(w http.ResponseWriter, r *http.Request) {
    var req authRequest; if decode(r,&req)!=nil || req.Name=="" || req.Email=="" || len(req.Password)<8 { errorJSON(w,400,"name, email and password (8+ chars) are required"); return }
    hash, err := bcrypt.GenerateFromPassword([]byte(req.Password),bcrypt.DefaultCost); if err!=nil { errorJSON(w,500,"could not hash password"); return }
    var id uuid.UUID; err=a.db.QueryRow(`INSERT INTO owners(name,email,phone,password_hash) VALUES($1,$2,$3,$4) RETURNING id`,req.Name,strings.ToLower(req.Email),req.Phone,string(hash)).Scan(&id)
    if err!=nil { errorJSON(w,409,"email already registered"); return }
    token,err:=a.token(id.String()); if err!=nil { errorJSON(w,500,"could not create token"); return }
    writeJSON(w,201,map[string]any{"owner_id":id,"name":strings.TrimSpace(req.Name),"token":token})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
    var req loginRequest; if decode(r,&req)!=nil { errorJSON(w,400,"invalid request"); return }
    var id uuid.UUID; var hash,name string; err:=a.db.QueryRow(`SELECT id,name,password_hash FROM owners WHERE email=$1`,strings.ToLower(req.Email)).Scan(&id,&name,&hash)
    if err!=nil || bcrypt.CompareHashAndPassword([]byte(hash),[]byte(req.Password))!=nil { errorJSON(w,401,"invalid email or password"); return }
    token,err:=a.token(id.String()); if err!=nil { errorJSON(w,500,"could not create token"); return }; writeJSON(w,200,map[string]any{"owner_id":id,"name":strings.TrimSpace(name),"token":token})
}
func (a *App) token(ownerID string)(string,error){ now:=time.Now(); claims:=OwnerClaims{OwnerID:ownerID,RegisteredClaims:jwt.RegisteredClaims{Issuer:"messmate",Subject:ownerID,IssuedAt:jwt.NewNumericDate(now),ExpiresAt:jwt.NewNumericDate(now.Add(a.ttl)),ID:randomID()}}; return jwt.NewWithClaims(jwt.SigningMethodHS256,claims).SignedString(a.jwtSecret) }
func randomID() string { b:=make([]byte,12); _,_=rand.Read(b); return base64.RawURLEncoding.EncodeToString(b) }
func (a *App) auth(next http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){ h:=r.Header.Get("Authorization"); if !strings.HasPrefix(h,"Bearer "){errorJSON(w,401,"missing bearer token");return}; token,err:=jwt.ParseWithClaims(strings.TrimPrefix(h,"Bearer "),&OwnerClaims{},func(t *jwt.Token)(any,error){if t.Method!=jwt.SigningMethodHS256{return nil,errors.New("invalid signing method")};return a.jwtSecret,nil}); if err!=nil || !token.Valid {errorJSON(w,401,"invalid or expired token");return}; c,ok:=token.Claims.(*OwnerClaims);if !ok{errorJSON(w,401,"invalid token claims");return}; oid,err:=uuid.Parse(c.OwnerID);if err!=nil{errorJSON(w,401,"invalid owner id");return}; next(w,r.WithContext(context.WithValue(r.Context(),ownerKey,oid))) } }
func ownerID(r *http.Request)(uuid.UUID,error){v:=r.Context().Value(ownerKey); id,ok:=v.(uuid.UUID);if !ok{return uuid.Nil,errors.New("owner missing")};return id,nil}

func (a *App) dashboard(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r)
    monthStart,_,monthErr:=parseMonth(r.URL.Query().Get("month"))
    if monthErr!=nil { errorJSON(w,400,monthErr.Error()); return }
    nextMonth:=monthStart.AddDate(0,1,0)
    var active,expiring,pending,attendance int
    var collections float64
    _=a.db.QueryRow(`SELECT COUNT(DISTINCT c.id) FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.owner_id=$1 AND c.created_at < $3 AND s.start_date < $3::date AND s.end_date >= $2::date AND c.active`,oid,monthStart,nextMonth).Scan(&active)
    if monthStart.Format("2006-01")==time.Now().In(indiaLocation).Format("2006-01") {
        _=a.db.QueryRow(`SELECT COUNT(*) FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.owner_id=$1 AND s.end_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date`,oid).Scan(&expiring)
        _=a.db.QueryRow(`SELECT COALESCE(SUM(p.amount),0) FROM payments p JOIN consumers c ON c.id=p.consumer_id WHERE c.owner_id=$1 AND (p.paid_at AT TIME ZONE 'Asia/Kolkata')::date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date`,oid).Scan(&collections)
        _=a.db.QueryRow(`SELECT COUNT(*) FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id=$1 AND at.attendance_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date`,oid).Scan(&attendance)
    }
    _=a.db.QueryRow(`SELECT COUNT(*) FROM consumers c JOIN LATERAL (SELECT amount,amount_paid FROM subscriptions WHERE consumer_id=c.id AND start_date < $3::date AND end_date >= $2::date ORDER BY start_date DESC,created_at DESC LIMIT 1) s ON TRUE WHERE c.owner_id=$1 AND c.created_at < $3 AND s.amount_paid<s.amount`,oid,monthStart,nextMonth).Scan(&pending)
    writeJSON(w,200,map[string]any{"active_consumers":active,"expiring_today":expiring,"pending_payments":pending,"today_collections":collections,"today_attendance":attendance})
}

func normalizeMealPlan(plan string) (string,error) {
    plan=strings.ToLower(strings.TrimSpace(plan)); if plan==""||plan=="all" { return "all",nil }
    allowed:=map[string]bool{"breakfast":true,"lunch":true,"dinner":true}; seen:=map[string]bool{}
    var meals []string
    for _,m:=range strings.Split(plan,","){m=strings.TrimSpace(m);if !allowed[m]{return "",errors.New("meal_plan must contain breakfast, lunch or dinner")};if !seen[m]{seen[m]=true;meals=append(meals,m)}}
    if len(meals)==0{return "",errors.New("at least one meal is required")}; return strings.Join(meals,","),nil
}
func paymentStatus(total,paid float64) string { if paid<=0{return "pending"}; if paid>=total{return "paid"}; return "partial" }

func (a *App) createConsumer(w http.ResponseWriter,r *http.Request){
    oid,ownerErr:=ownerID(r)
    if ownerErr!=nil { log.Printf("createConsumer owner lookup failed: %v",ownerErr); errorJSON(w,401,"authentication required"); return }
    var req consumerRequest
    if err:=decode(r,&req);err!=nil { log.Printf("createConsumer invalid JSON: %v",err); errorJSON(w,400,"invalid request body"); return }
    if req.Name==""||req.Phone=="" { log.Printf("createConsumer validation failed: name or phone missing"); errorJSON(w,400,"name and phone are required"); return }
    plan,err:=normalizeMealPlan(req.MealPlan)
    if err!=nil { log.Printf("createConsumer meal plan validation failed: %v",err); errorJSON(w,400,err.Error()); return }
    if req.ConsumerID=="" { req.ConsumerID="MM-"+strings.ToUpper(randomID()[:8]) }
    if req.StartDate=="" { log.Printf("createConsumer validation failed: start_date missing"); errorJSON(w,400,"start_date is required"); return }
    if req.EndDate=="" { startTmp,e:=time.Parse("2006-01-02",req.StartDate); if e!=nil { log.Printf("createConsumer invalid start_date %q: %v",req.StartDate,e); errorJSON(w,400,"invalid start_date"); return }; req.EndDate=time.Date(startTmp.Year(),startTmp.Month()+1,0,0,0,0,0,startTmp.Location()).Format("2006-01-02") }
    startDate,e:=time.Parse("2006-01-02",req.StartDate)
    if e!=nil { log.Printf("createConsumer invalid start_date %q: %v",req.StartDate,e); errorJSON(w,400,"invalid start_date"); return }
    endDate,e:=time.Parse("2006-01-02",req.EndDate)
    if e!=nil||endDate.Before(startDate) { log.Printf("createConsumer invalid end_date %q for start_date %q: %v",req.EndDate,req.StartDate,e); errorJSON(w,400,"invalid end_date"); return }
    if req.Amount<=0 { log.Printf("createConsumer validation failed: amount must be positive (amount=%v)",req.Amount); errorJSON(w,400,"amount must be greater than 0"); return }
    if req.AmountPaid<0||req.AmountPaid>req.Amount { log.Printf("createConsumer validation failed: amount_paid=%v amount=%v",req.AmountPaid,req.Amount); errorJSON(w,400,"amount paid must be between 0 and total amount"); return }
    tx,err:=a.db.Begin()
    if err!=nil { log.Printf("createConsumer begin transaction failed: %v",err); errorJSON(w,500,"could not start transaction"); return }
    defer tx.Rollback()
    var id,qr,sid uuid.UUID
    err=tx.QueryRow(`INSERT INTO consumers(owner_id,consumer_id,name,phone,meal_plan) VALUES($1,$2,$3,$4,$5) RETURNING id,qr_token`,oid,req.ConsumerID,req.Name,req.Phone,plan).Scan(&id,&qr)
    if err!=nil { log.Printf("createConsumer consumer insert failed: %v",err); if strings.Contains(strings.ToLower(err.Error()),"duplicate key") { errorJSON(w,409,"consumer ID already exists for this owner") } else { errorJSON(w,500,"database error while creating consumer") }; return }
    status:=paymentStatus(req.Amount,req.AmountPaid)
    month:=startDate.Format("January 2006")
    err=tx.QueryRow(`INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,amount_paid,month_label,payment_status) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,id,startDate,endDate,req.Amount,req.AmountPaid,month,status).Scan(&sid)
    if err!=nil { log.Printf("createConsumer subscription insert failed: %v",err); errorJSON(w,500,"database error while creating subscription"); return }
    if req.AmountPaid>0 { if _,err=tx.Exec(`INSERT INTO payments(consumer_id,subscription_id,amount,method) VALUES($1,$2,$3,'cash')`,id,sid,req.AmountPaid); err!=nil { log.Printf("createConsumer initial payment insert failed: %v",err); errorJSON(w,500,"database error while recording initial payment"); return } }
    if err=tx.Commit();err!=nil { log.Printf("createConsumer transaction commit failed: %v",err); errorJSON(w,500,"database error while saving consumer"); return }
    log.Printf("createConsumer success: consumer_id=%s owner_id=%s",id,oid)
    writeJSON(w,201,map[string]any{"id":id,"consumer_id":req.ConsumerID,"qr_token":qr,"subscription_id":sid,"start_date":req.StartDate,"end_date":req.EndDate,"amount":req.Amount,"amount_paid":req.AmountPaid,"payment_status":status})
}
func (a *App) listConsumers(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);monthStart,_,monthErr:=parseMonth(r.URL.Query().Get("month"));if monthErr!=nil{errorJSON(w,400,monthErr.Error());return};nextMonth:=monthStart.AddDate(0,1,0)
    // Prefer a subscription covering the selected month, but never hide a consumer
    // merely because they have no subscription overlapping that month. The fallback
    // keeps their latest subscription visible, including backfilled historical plans.
    rows,err:=a.db.Query(`SELECT c.id,c.consumer_id,c.name,c.phone,c.meal_plan,c.active,c.qr_token,COALESCE(s.id,'00000000-0000-0000-0000-000000000000'),COALESCE(s.start_date,'0001-01-01'),COALESCE(s.end_date,'0001-01-01'),COALESCE(s.amount,0),COALESCE(NULLIF(s.monthly_amount,0),s.amount),COALESCE(s.amount_paid,0),COALESCE(s.month_label,''),COALESCE(s.payment_status,'pending') FROM consumers c LEFT JOIN LATERAL (SELECT * FROM subscriptions WHERE consumer_id=c.id ORDER BY CASE WHEN start_date < $3::date AND end_date >= $2::date THEN 0 ELSE 1 END,start_date DESC,created_at DESC LIMIT 1) s ON TRUE WHERE c.owner_id=$1 AND (c.created_at < $3 OR EXISTS (SELECT 1 FROM subscriptions sx WHERE sx.consumer_id=c.id AND sx.start_date < $3::date AND sx.end_date >= $2::date)) ORDER BY c.created_at DESC`,oid,monthStart,nextMonth);if err!=nil{log.Printf("listConsumers query failed: %v",err);errorJSON(w,500,"database error");return};defer rows.Close()
    out:=[]map[string]any{};asOf:=nextMonth.Add(-time.Second);today:=time.Now().In(indiaLocation);if asOf.After(today){asOf=today};for rows.Next(){var id,qr,sid uuid.UUID;var cid,name,phone,plan,month,status string;var active bool;var start,end time.Time;var amount,monthly,paid float64;if err:=rows.Scan(&id,&cid,&name,&phone,&plan,&active,&qr,&sid,&start,&end,&amount,&monthly,&paid,&month,&status);err!=nil{continue};status=displayedPaymentStatus(paid,amount,end,asOf);subscription:=map[string]any{"id":sid,"start_date":start.Format("2006-01-02"),"end_date":end.Format("2006-01-02"),"amount":amount,"monthly_amount":monthly,"amount_paid":paid,"month_label":month,"payment_status":status};out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"meal_plan":plan,"active":active,"qr_token":qr,"subscription":subscription})};writeJSON(w,200,out)
}
func (a *App) getConsumer(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return};var cid,name,phone,plan string;var active bool;var qr uuid.UUID
    err=a.db.QueryRow(`SELECT consumer_id,name,phone,meal_plan,active,qr_token FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&cid,&name,&phone,&plan,&active,&qr);if err!=nil{errorJSON(w,404,"consumer not found");return};writeJSON(w,200,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"meal_plan":plan,"active":active,"qr_token":qr})
}
func (a *App) updateConsumer(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return};var req consumerRequest;if decode(r,&req)!=nil{errorJSON(w,400,"invalid request");return};plan:=req.MealPlan;if plan!=""{var e error;plan,e=normalizeMealPlan(plan);if e!=nil{errorJSON(w,400,e.Error());return}}
    tx,err:=a.db.Begin();if err!=nil{errorJSON(w,500,"could not start update");return};defer tx.Rollback()
    res,err:=tx.Exec(`UPDATE consumers SET name=COALESCE(NULLIF($1,''),name),phone=COALESCE(NULLIF($2,''),phone),meal_plan=COALESCE(NULLIF($3,''),meal_plan) WHERE id=$4 AND owner_id=$5`,req.Name,req.Phone,plan,id,oid);if err!=nil{errorJSON(w,500,"database error");return};n,_:=res.RowsAffected();if n==0{errorJSON(w,404,"consumer not found");return}
    if req.Amount>0 { if _,err=tx.Exec(`UPDATE subscriptions SET monthly_amount=$1 WHERE id=(SELECT id FROM subscriptions WHERE consumer_id=$2 ORDER BY end_date DESC LIMIT 1)`,req.Amount,id);err!=nil{errorJSON(w,500,"could not update monthly amount");return} }
    if err=tx.Commit();err!=nil{errorJSON(w,500,"could not save consumer");return};a.getConsumer(w,r)
}
func (a *App) deleteConsumer(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return};res,err:=a.db.Exec(`DELETE FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid);if err!=nil{errorJSON(w,500,"database error");return};n,_:=res.RowsAffected();if n==0{errorJSON(w,404,"consumer not found");return};w.WriteHeader(http.StatusNoContent)}
type workerRequest struct { Name string `json:"name"`; DateJoined string `json:"date_joined"`; Shift string `json:"shift"` }
type workerAttendanceRequest struct { QRToken string `json:"qr_token"` }
func (a *App) scanWorkerAttendance(w http.ResponseWriter,r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return}
 var req workerAttendanceRequest
 if decode(r,&req)!=nil{errorJSON(w,400,"invalid request");return}
 token,err:=uuid.Parse(req.QRToken);if err!=nil{errorJSON(w,400,"invalid worker QR token");return}
 var workerID uuid.UUID;var name,shift string
 err=a.db.QueryRow("SELECT id,name,shift FROM workers WHERE qr_token=$1 AND owner_id=$2",token,oid).Scan(&workerID,&name,&shift)
 if err!=nil{errorJSON(w,404,"worker not found");return}
 result,err:=a.recordWorkerAttendance(workerID,shift)
 if err!=nil{errorJSON(w,500,"could not record worker attendance");return}
 result["worker_id"]=workerID;result["name"]=name;result["shift"]=shift
 writeJSON(w,201,result)
}
func (a *App) recordWorkerAttendance(workerID uuid.UUID,shift string)(map[string]any,error){
 tx,err:=a.db.Begin();if err!=nil{return nil,err};defer tx.Rollback()
 // Serialize scans for the same worker so two rapid scans cannot create overlapping sessions.
 var locked uuid.UUID
 if err=tx.QueryRow("SELECT id FROM workers WHERE id=$1 FOR UPDATE",workerID).Scan(&locked);err!=nil{return nil,err}
 var id uuid.UUID;var inTime time.Time;var outTime sql.NullTime
 err=tx.QueryRow(`SELECT id,in_time,out_time FROM worker_attendance_sessions WHERE worker_id=$1 AND attendance_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date AND out_time IS NULL ORDER BY in_time DESC LIMIT 1 FOR UPDATE`,workerID).Scan(&id,&inTime,&outTime)
 status:="IN recorded"
 if err==nil {
   if err=tx.QueryRow("UPDATE worker_attendance_sessions SET out_time=NOW() WHERE id=$1 RETURNING out_time",id).Scan(&outTime);err!=nil{return nil,err}
   status="OUT recorded"
 } else if err==sql.ErrNoRows {
   if err=tx.QueryRow("INSERT INTO worker_attendance_sessions(worker_id,attendance_date,shift) VALUES($1,(NOW() AT TIME ZONE 'Asia/Kolkata')::date,$2) RETURNING id,in_time,out_time",workerID,shift).Scan(&id,&inTime,&outTime);err!=nil{return nil,err}
 } else { return nil,err }
 if err=tx.Commit();err!=nil{return nil,err}
 var minutes int64
 if outTime.Valid {minutes=int64(outTime.Time.Sub(inTime).Minutes());if minutes<0{minutes=0}}
 var total int64
 _=a.db.QueryRow(`SELECT COALESCE(SUM(GREATEST(0,EXTRACT(EPOCH FROM (COALESCE(out_time,NOW())-in_time))/60)),0)::bigint FROM worker_attendance_sessions WHERE worker_id=$1 AND attendance_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date`,workerID).Scan(&total)
 return map[string]any{"attendance_id":id,"in_time":inTime,"out_time":nullableWorkerTime(outTime),"worked_minutes":minutes,"total_worked_minutes":total,"status":status},nil
}
func nullableWorkerTime(t sql.NullTime) any { if !t.Valid{return nil};return t.Time }
func (a *App) todayWorkerAttendance(w http.ResponseWriter,r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return}
 rows,err:=a.db.Query(`SELECT w.id,w.name,w.shift,MAX(s.in_time),MAX(s.out_time),COALESCE(SUM(GREATEST(0,EXTRACT(EPOCH FROM (COALESCE(s.out_time,NOW())-s.in_time))/60)),0)::bigint,BOOL_OR(s.out_time IS NULL) FROM workers w JOIN worker_attendance_sessions s ON s.worker_id=w.id WHERE w.owner_id=$1 AND s.attendance_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date GROUP BY w.id,w.name,w.shift ORDER BY MAX(s.in_time) DESC`,oid)
 if err!=nil{errorJSON(w,500,"could not load worker attendance");return};defer rows.Close()
 out:=[]map[string]any{}
 for rows.Next(){var id uuid.UUID;var name,shift string;var inTime time.Time;var outTime sql.NullTime;var total int64;var onShift bool;if rows.Scan(&id,&name,&shift,&inTime,&outTime,&total,&onShift)==nil{status:="out";if onShift{status="in"};out=append(out,map[string]any{"worker_id":id,"name":name,"shift":shift,"in_time":inTime,"out_time":nullableWorkerTime(outTime),"worked_minutes":total,"total_worked_minutes":total,"status":status})}}
 writeJSON(w,200,out)
}
func (a *App) workerAttendanceHistory(w http.ResponseWriter,r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return}
 id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid worker id");return}
 var name,shift string
 if err=a.db.QueryRow("SELECT name,shift FROM workers WHERE id=$1 AND owner_id=$2",id,oid).Scan(&name,&shift);err!=nil{errorJSON(w,404,"worker not found");return}
 month:=strings.TrimSpace(r.URL.Query().Get("month"))
 query:=`SELECT attendance_date::text,id,in_time,out_time,GREATEST(0,EXTRACT(EPOCH FROM (COALESCE(out_time,NOW())-in_time))/60)::bigint FROM worker_attendance_sessions WHERE worker_id=$1`
 args:=[]any{id}
 if month!="" { if len(month)!=7 || month[4]!='-' {errorJSON(w,400,"month must be YYYY-MM");return};query+=` AND to_char(attendance_date,'YYYY-MM')=$2`;args=append(args,month) } else { query+=` AND attendance_date >= (NOW() AT TIME ZONE 'Asia/Kolkata')::date - INTERVAL '365 days'` }
 query+=` ORDER BY attendance_date DESC,in_time ASC`
 rows,err:=a.db.Query(query,args...);if err!=nil{errorJSON(w,500,"could not load worker history");return};defer rows.Close()
 days:=[]map[string]any{};byDate:=map[string]map[string]any{}
 for rows.Next(){var date string;var sessionID uuid.UUID;var inTime time.Time;var outTime sql.NullTime;var minutes int64
  if err=rows.Scan(&date,&sessionID,&inTime,&outTime,&minutes);err!=nil{errorJSON(w,500,"could not read worker history");return}
  day:=byDate[date];if day==nil{day=map[string]any{"date":date,"in_time":inTime,"out_time":nullableWorkerTime(outTime),"worked_minutes":int64(0),"total_worked_minutes":int64(0),"sessions":[]map[string]any{}};byDate[date]=day;days=append(days,day)}
  sessions:=day["sessions"].([]map[string]any);sessions=append(sessions,map[string]any{"id":sessionID,"in_time":inTime,"out_time":nullableWorkerTime(outTime),"worked_minutes":minutes,"status":func()string{if outTime.Valid{return "complete"};return "in"}()});day["sessions"]=sessions
  total:=day["total_worked_minutes"].(int64)+minutes;day["total_worked_minutes"]=total;day["worked_minutes"]=total;day["session_count"]=len(sessions);day["out_time"]=nullableWorkerTime(outTime)
 }
 if err=rows.Err();err!=nil{errorJSON(w,500,"could not read worker history");return}
 writeJSON(w,200,map[string]any{"worker_id":id,"name":name,"shift":shift,"attendance":days})
}
func (a *App) deleteWorker(w http.ResponseWriter,r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return}
 id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid worker id");return}
 result,err:=a.db.Exec("DELETE FROM workers WHERE id=$1 AND owner_id=$2",id,oid)
 if err!=nil{errorJSON(w,500,"could not delete worker");return}
 n,err:=result.RowsAffected();if err!=nil{errorJSON(w,500,"could not confirm worker deletion");return}
 if n==0{errorJSON(w,404,"worker not found");return}
 w.WriteHeader(http.StatusNoContent)
}

func (a *App) listWorkers(w http.ResponseWriter, r *http.Request) {
 oid,err:=ownerID(r); if err!=nil { errorJSON(w,401,"authentication required"); return }
 rows,err:=a.db.Query(`SELECT id,name,date_joined::text,shift,qr_token FROM workers WHERE owner_id=$1 ORDER BY created_at DESC`,oid); if err!=nil { errorJSON(w,500,"could not list workers"); return }; defer rows.Close()
 out:=[]map[string]any{}; for rows.Next(){var id,token uuid.UUID;var name,dateJoined,shift string;if rows.Scan(&id,&name,&dateJoined,&shift,&token)!=nil{errorJSON(w,500,"could not read workers");return};out=append(out,map[string]any{"id":id,"name":name,"date_joined":dateJoined,"shift":shift,"qr_token":token})};writeJSON(w,200,out)
}
func (a *App) createWorker(w http.ResponseWriter, r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return};var req workerRequest
 if decode(r,&req)!=nil||strings.TrimSpace(req.Name)==""||req.Shift==""{errorJSON(w,400,"name, date_joined and shift are required");return}
 joined,err:=time.Parse("2006-01-02",req.DateJoined);if err!=nil{errorJSON(w,400,"date_joined must be YYYY-MM-DD");return}
 var id,token uuid.UUID;var dateJoined string
 err=a.db.QueryRow(`INSERT INTO workers(owner_id,name,date_joined,shift) VALUES($1,$2,$3,$4) RETURNING id,qr_token,date_joined::text`,oid,strings.TrimSpace(req.Name),joined,strings.TrimSpace(req.Shift)).Scan(&id,&token,&dateJoined)
 if err!=nil{errorJSON(w,500,"could not create worker");return};writeJSON(w,201,map[string]any{"id":id,"name":strings.TrimSpace(req.Name),"date_joined":dateJoined,"shift":req.Shift,"qr_token":token})
}
func (a *App) workerQR(w http.ResponseWriter,r *http.Request) {
 oid,err:=ownerID(r);if err!=nil{errorJSON(w,401,"authentication required");return};id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid worker id");return}
 var token uuid.UUID;var name,dateJoined,shift string
 if err=a.db.QueryRow(`SELECT qr_token,name,date_joined::text,shift FROM workers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&token,&name,&dateJoined,&shift);err!=nil{errorJSON(w,404,"worker not found");return}
 png,err:=qrcode.Encode(token.String(),qrcode.Medium,256);if err!=nil{errorJSON(w,500,"could not generate QR");return};writeJSON(w,200,map[string]any{"id":id,"name":name,"date_joined":dateJoined,"shift":shift,"qr_token":token,"format":"png","image_base64":base64.StdEncoding.EncodeToString(png)})
}
func (a *App) consumerQR(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return};var token uuid.UUID;var name,cid string;if err=a.db.QueryRow(`SELECT qr_token,name,consumer_id FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&token,&name,&cid);err!=nil{errorJSON(w,404,"consumer not found");return};png,err:=qrcode.Encode(token.String(),qrcode.Medium,256);if err!=nil{errorJSON(w,500,"could not generate QR");return};writeJSON(w,200,map[string]any{"consumer_id":cid,"name":name,"qr_token":token,"format":"png","image_base64":base64.StdEncoding.EncodeToString(png)})}

func (a *App) createSubscription(w http.ResponseWriter, r *http.Request) {
    oid, ownerErr := ownerID(r)
    if ownerErr != nil {
        errorJSON(w, http.StatusUnauthorized, "authentication required")
        return
    }

    var req subscriptionRequest
    if decode(r, &req) != nil {
        errorJSON(w, http.StatusBadRequest, "invalid request")
        return
    }
    cid, err := uuid.Parse(req.ConsumerID)
    if err != nil {
        errorJSON(w, http.StatusBadRequest, "consumer_id must be a UUID")
        return
    }
    start, err := time.Parse("2006-01-02", req.StartDate)
    if err != nil {
        errorJSON(w, http.StatusBadRequest, "invalid start_date")
        return
    }
    end, err := time.Parse("2006-01-02", req.EndDate)
    if err != nil || end.Before(start) {
        errorJSON(w, http.StatusBadRequest, "invalid end_date")
        return
    }
    if req.Amount <= 0 || req.AmountPaid < 0 || req.AmountPaid > req.Amount {
        errorJSON(w, http.StatusBadRequest, "amount must be positive and amount_paid must be between zero and amount")
        return
    }
    req.PaymentStatus = paymentStatus(req.Amount, req.AmountPaid)
    if req.MonthLabel == "" {
        req.MonthLabel = start.Format("January 2006")
    }

    tx, err := a.db.Begin()
    if err != nil {
        errorJSON(w, http.StatusInternalServerError, "could not start subscription")
        return
    }
    defer tx.Rollback()

    var sid uuid.UUID
    err = tx.QueryRow(`INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,amount_paid,month_label,payment_status)
        SELECT $1,$2,$3,$4,$5,$6,$7
        WHERE EXISTS(SELECT 1 FROM consumers WHERE id=$1 AND owner_id=$8)
        RETURNING id`, cid, start, end, req.Amount, req.AmountPaid, req.MonthLabel, req.PaymentStatus, oid).Scan(&sid)
    if err != nil {
        errorJSON(w, http.StatusNotFound, "consumer not found or subscription could not be created")
        return
    }

    if req.AmountPaid > 0 {
        if _, err = tx.Exec(`INSERT INTO payments(consumer_id,subscription_id,amount,method)
            VALUES($1,$2,$3,'cash')`, cid, sid, req.AmountPaid); err != nil {
            log.Printf("createSubscription payment insert failed: %v", err)
            errorJSON(w, http.StatusInternalServerError, "could not record initial payment")
            return
        }
    }
    if err = tx.Commit(); err != nil {
        log.Printf("createSubscription commit failed: %v", err)
        errorJSON(w, http.StatusInternalServerError, "could not save subscription")
        return
    }

    writeJSON(w, http.StatusCreated, map[string]any{
        "id": sid,
        "consumer_id": cid,
        "start_date": req.StartDate,
        "end_date": req.EndDate,
        "amount": req.Amount,
        "amount_paid": req.AmountPaid,
        "month_label": req.MonthLabel,
        "payment_status": req.PaymentStatus,
    })
}
func (a *App) expiringToday(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(fmt.Sprintf(`SELECT c.id,c.consumer_id,c.name,c.phone,s.amount,s.amount_paid,s.end_date FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.owner_id='%s'::uuid AND s.end_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date ORDER BY c.name`,oid.String()));if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id uuid.UUID;var cid,name,phone string;var amount,paid float64;var end time.Time;if rows.Scan(&id,&cid,&name,&phone,&amount,&paid,&end)==nil{status:=displayedPaymentStatus(paid,amount,end,time.Now());out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"amount":amount,"amount_paid":paid,"payment_status":status,"reminder_eligible":status!="paid"})}};writeJSON(w,200,out)}
func (a *App) pendingPayments(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);monthStart,_,monthErr:=parseMonth(r.URL.Query().Get("month"));if monthErr!=nil{errorJSON(w,400,monthErr.Error());return};nextMonth:=monthStart.AddDate(0,1,0);rows,err:=a.db.Query(`SELECT c.id,c.consumer_id,c.name,c.phone,s.end_date,s.amount,s.amount_paid FROM consumers c JOIN LATERAL (SELECT * FROM subscriptions WHERE consumer_id=c.id AND start_date < $3::date AND end_date >= $2::date ORDER BY start_date DESC,created_at DESC LIMIT 1) s ON TRUE WHERE c.owner_id=$1 AND c.created_at < $3 AND s.amount_paid<s.amount ORDER BY s.end_date`,oid,monthStart,nextMonth);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};asOf:=nextMonth.Add(-time.Second);today:=time.Now().In(indiaLocation);if asOf.After(today){asOf=today};for rows.Next(){var id uuid.UUID;var cid,name,phone string;var end time.Time;var amount,paid float64;if rows.Scan(&id,&cid,&name,&phone,&end,&amount,&paid)==nil{out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"end_date":end.Format("2006-01-02"),"amount":amount,"amount_paid":paid,"due":amount-paid,"payment_status":displayedPaymentStatus(paid,amount,end,asOf)})}};writeJSON(w,200,out)}
func (a *App) recentPayments(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);monthStart,_,monthErr:=parseMonth(r.URL.Query().Get("month"));if monthErr!=nil{errorJSON(w,400,monthErr.Error());return};nextMonth:=monthStart.AddDate(0,1,0);rows,err:=a.db.Query(`SELECT p.id,c.consumer_id,c.name,p.amount,p.method,COALESCE(p.reference,''),p.paid_at FROM payments p JOIN consumers c ON c.id=p.consumer_id WHERE c.owner_id=$1 AND p.paid_at >= $2 AND p.paid_at < $3 ORDER BY p.paid_at DESC LIMIT 50`,oid,monthStart,nextMonth);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id uuid.UUID;var cid,name,method,ref string;var amount float64;var paidAt time.Time;if rows.Scan(&id,&cid,&name,&amount,&method,&ref,&paidAt)==nil{out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"amount":amount,"method":method,"reference":ref,"paid_at":paidAt})}};writeJSON(w,200,out)}
func (a *App) createPayment(w http.ResponseWriter, r *http.Request) {
    oid, ownerErr := ownerID(r)
    if ownerErr != nil {
        errorJSON(w, http.StatusUnauthorized, "authentication required")
        return
    }

    var req paymentRequest
    if decode(r, &req) != nil || req.Amount <= 0 {
        errorJSON(w, http.StatusBadRequest, "consumer_id and positive amount are required")
        return
    }
    cid, err := uuid.Parse(req.ConsumerID)
    if err != nil {
        errorJSON(w, http.StatusBadRequest, "invalid consumer_id")
        return
    }
    if req.Method == "" {
        req.Method = "cash"
    }
    switch req.Method {
    case "cash", "upi", "bank", "card":
    default:
        errorJSON(w, http.StatusBadRequest, "method must be cash, upi, bank or card")
        return
    }

    // Allow backdating a payment to the date the money was actually received.
    paidAt := time.Now().In(indiaLocation)
    if req.PaidAt != "" {
        parsed, parseErr := time.ParseInLocation("2006-01-02", req.PaidAt, indiaLocation)
        if parseErr != nil {
            errorJSON(w, http.StatusBadRequest, "paid_at must use YYYY-MM-DD format")
            return
        }
        paidAt = parsed
    }

    tx, err := a.db.Begin()
    if err != nil {
        errorJSON(w, http.StatusInternalServerError, "could not start payment")
        return
    }
    defer tx.Rollback()

    var sid uuid.UUID
    var total, paid float64
    if req.SubscriptionID == "" {
        err = tx.QueryRow(`SELECT s.id, s.amount, s.amount_paid
            FROM subscriptions s
            JOIN consumers c ON c.id = s.consumer_id
            WHERE s.consumer_id = $1 AND c.owner_id = $2
            ORDER BY s.end_date DESC, s.created_at DESC
            LIMIT 1 FOR UPDATE OF s`, cid, oid).Scan(&sid, &total, &paid)
    } else {
        sid, err = uuid.Parse(req.SubscriptionID)
        if err != nil {
            errorJSON(w, http.StatusBadRequest, "invalid subscription_id")
            return
        }
        err = tx.QueryRow(`SELECT s.amount, s.amount_paid
            FROM subscriptions s
            JOIN consumers c ON c.id = s.consumer_id
            WHERE s.id = $1 AND s.consumer_id = $2 AND c.owner_id = $3
            FOR UPDATE OF s`, sid, cid, oid).Scan(&total, &paid)
    }
    if err != nil {
        errorJSON(w, http.StatusNotFound, "subscription not found")
        return
    }

    remaining := total - paid
    if req.Amount > remaining+0.0001 {
        errorJSON(w, http.StatusBadRequest, fmt.Sprintf("payment exceeds remaining due of %.2f", remaining))
        return
    }

    var pid uuid.UUID
    if err = tx.QueryRow(`INSERT INTO payments(consumer_id,subscription_id,amount,method,reference,paid_at)
        VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, cid, sid, req.Amount, req.Method, req.Reference, paidAt).Scan(&pid); err != nil {
        log.Printf("createPayment insert failed: %v", err)
        errorJSON(w, http.StatusInternalServerError, "payment failed")
        return
    }

    newPaid := paid + req.Amount
    if _, err = tx.Exec(`UPDATE subscriptions SET amount_paid=$1,payment_status=$2 WHERE id=$3`,
        newPaid, paymentStatus(total, newPaid), sid); err != nil {
        log.Printf("createPayment subscription update failed: %v", err)
        errorJSON(w, http.StatusInternalServerError, "could not update subscription balance")
        return
    }
    if err = tx.Commit(); err != nil {
        log.Printf("createPayment commit failed: %v", err)
        errorJSON(w, http.StatusInternalServerError, "could not save payment")
        return
    }

    writeJSON(w, http.StatusCreated, map[string]any{
        "id": pid,
        "consumer_id": cid,
        "subscription_id": sid,
        "amount": req.Amount,
        "amount_paid": newPaid,
        "due": total - newPaid,
        "method": req.Method,
    })
}
func (a *App) scanAttendance(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);var req attendanceRequest;if decode(r,&req)!=nil{errorJSON(w,400,"invalid request");return};if req.Meal!="breakfast"&&req.Meal!="lunch"&&req.Meal!="dinner"{errorJSON(w,400,"meal must be breakfast, lunch or dinner");return};token,err:=uuid.Parse(req.QRToken);if err!=nil{errorJSON(w,400,"invalid qr_token");return};var cid uuid.UUID;var consumerID,name,plan string;var active bool;err=a.db.QueryRow(`SELECT c.id,c.consumer_id,c.name,c.meal_plan,c.active FROM consumers c WHERE c.qr_token=$1 AND c.owner_id=$2`,token,oid).Scan(&cid,&consumerID,&name,&plan,&active);if err!=nil{errorJSON(w,404,"consumer not found");return};if !active{errorJSON(w,403,"consumer is inactive");return};if plan!="all"&&!containsMeal(plan,req.Meal){errorJSON(w,403,"consumer is not subscribed to this meal");return}
    var end time.Time;err=a.db.QueryRow(`SELECT end_date FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1`,cid).Scan(&end);if err!=nil||dateBeforeInIndia(end, time.Now()){errorJSON(w,403,"consumer subscription is not active");return}
    var aid uuid.UUID;err=a.db.QueryRow(`INSERT INTO attendance(consumer_id,attendance_date,meal) VALUES($1,(NOW() AT TIME ZONE 'Asia/Kolkata')::date,$2) ON CONFLICT(consumer_id,attendance_date,meal) DO NOTHING RETURNING id`,cid,req.Meal).Scan(&aid);if err!=nil{errorJSON(w,409,"attendance already marked for this meal");return};writeJSON(w,201,map[string]any{"attendance_id":aid,"consumer_id":consumerID,"name":name,"meal":req.Meal,"date":time.Now().In(indiaLocation).Format("2006-01-02"),"status":"marked"})
}
func containsMeal(plan,meal string) bool { for _,m:=range strings.Split(plan,","){if m==meal{return true}};return false }
func (a *App) todayAttendance(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(fmt.Sprintf(`SELECT c.consumer_id,c.name,at.meal,at.scanned_at FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id='%s'::uuid AND at.attendance_date=(NOW() AT TIME ZONE 'Asia/Kolkata')::date ORDER BY at.scanned_at DESC`,oid.String()));if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var cid,name,meal string;var scanned time.Time;if rows.Scan(&cid,&name,&meal,&scanned)==nil{out=append(out,map[string]any{"consumer_id":cid,"name":name,"meal":meal,"scanned_at":scanned})}};writeJSON(w,200,out)}
func logging(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){start:=time.Now();next.ServeHTTP(w,r);log.Printf("%s %s %s",r.Method,r.URL.Path,time.Since(start))})}
func cors(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Access-Control-Allow-Origin","*");w.Header().Set("Access-Control-Allow-Headers","Authorization, Content-Type");w.Header().Set("Access-Control-Allow-Methods","GET, POST, PUT, PATCH, DELETE, OPTIONS");if r.Method==http.MethodOptions{w.WriteHeader(http.StatusNoContent);return};next.ServeHTTP(w,r)})}
var _ = fmt.Sprintf
