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
type paymentRequest struct { ConsumerID string `json:"consumer_id"`; SubscriptionID string `json:"subscription_id"`; Amount float64 `json:"amount"`; Method string `json:"method"`; Reference string `json:"reference"` }
type attendanceRequest struct { QRToken string `json:"qr_token"`; Meal string `json:"meal"` }

func main() {
    dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")
    db, err := sql.Open("postgres", dsn); if err != nil { log.Fatal(err) }
    if err = db.Ping(); err != nil { log.Fatal("database: ", err) }
    if err = runMigrations(db); err != nil { log.Fatal("migrations: ", err) }
    ttlHours, _ := strconv.Atoi(getenv("JWT_TTL_HOURS", "24"))
    app := &App{db: db, jwtSecret: []byte(getenv("JWT_SECRET", "dev-secret-change-me")), ttl: time.Duration(ttlHours)*time.Hour}
    mux := http.NewServeMux()
    mux.HandleFunc("GET /health", app.health)
    mux.HandleFunc("POST /api/v1/auth/register", app.register)
    mux.HandleFunc("POST /api/v1/auth/login", app.login)
    mux.HandleFunc("GET /api/v1/dashboard", app.auth(app.dashboard))
    mux.HandleFunc("GET /api/v1/analytics", app.auth(app.monthlyAnalytics))
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
    mux.HandleFunc("POST /api/v1/attendance/scan", app.auth(app.scanAttendance))
    mux.HandleFunc("GET /api/v1/attendance/today", app.auth(app.todayAttendance))
    registerWeb(mux)
    handler := cors(logging(mux))
    port := getenv("PORT", "8080")
    log.Printf("MessMate listening on :%s", port)
    log.Fatal(http.ListenAndServe(":"+port, handler))
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
    writeJSON(w,201,map[string]any{"owner_id":id,"token":token})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
    var req loginRequest; if decode(r,&req)!=nil { errorJSON(w,400,"invalid request"); return }
    var id uuid.UUID; var hash string; err:=a.db.QueryRow(`SELECT id,password_hash FROM owners WHERE email=$1`,strings.ToLower(req.Email)).Scan(&id,&hash)
    if err!=nil || bcrypt.CompareHashAndPassword([]byte(hash),[]byte(req.Password))!=nil { errorJSON(w,401,"invalid email or password"); return }
    token,err:=a.token(id.String()); if err!=nil { errorJSON(w,500,"could not create token"); return }; writeJSON(w,200,map[string]any{"owner_id":id,"token":token})
}
func (a *App) token(ownerID string)(string,error){ now:=time.Now(); claims:=OwnerClaims{OwnerID:ownerID,RegisteredClaims:jwt.RegisteredClaims{Issuer:"messmate",Subject:ownerID,IssuedAt:jwt.NewNumericDate(now),ExpiresAt:jwt.NewNumericDate(now.Add(a.ttl)),ID:randomID()}}; return jwt.NewWithClaims(jwt.SigningMethodHS256,claims).SignedString(a.jwtSecret) }
func randomID() string { b:=make([]byte,12); _,_=rand.Read(b); return base64.RawURLEncoding.EncodeToString(b) }
func (a *App) auth(next http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){ h:=r.Header.Get("Authorization"); if !strings.HasPrefix(h,"Bearer "){errorJSON(w,401,"missing bearer token");return}; token,err:=jwt.ParseWithClaims(strings.TrimPrefix(h,"Bearer "),&OwnerClaims{},func(t *jwt.Token)(any,error){if t.Method!=jwt.SigningMethodHS256{return nil,errors.New("invalid signing method")};return a.jwtSecret,nil}); if err!=nil || !token.Valid {errorJSON(w,401,"invalid or expired token");return}; c,ok:=token.Claims.(*OwnerClaims);if !ok{errorJSON(w,401,"invalid token claims");return}; oid,err:=uuid.Parse(c.OwnerID);if err!=nil{errorJSON(w,401,"invalid owner id");return}; next(w,r.WithContext(context.WithValue(r.Context(),ownerKey,oid))) } }
func ownerID(r *http.Request)(uuid.UUID,error){v:=r.Context().Value(ownerKey); id,ok:=v.(uuid.UUID);if !ok{return uuid.Nil,errors.New("owner missing")};return id,nil}

func (a *App) dashboard(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r); var active,expiring,pending,attendance int; var collections float64
    _=a.db.QueryRow(`SELECT COUNT(*) FROM consumers WHERE owner_id=$1 AND active`,oid).Scan(&active)
    _=a.db.QueryRow(`SELECT COUNT(*) FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.owner_id=$1 AND s.end_date=CURRENT_DATE`,oid).Scan(&expiring)
    _=a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE c.owner_id=$1 AND s.payment_status <> 'paid' AND s.end_date>=CURRENT_DATE`,oid).Scan(&pending)
    _=a.db.QueryRow(`SELECT COALESCE(SUM(p.amount),0) FROM payments p JOIN consumers c ON c.id=p.consumer_id WHERE c.owner_id=$1 AND p.paid_at::date=CURRENT_DATE`,oid).Scan(&collections)
    _=a.db.QueryRow(`SELECT COUNT(*) FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id=$1 AND at.attendance_date=CURRENT_DATE`,oid).Scan(&attendance)
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
    oid,_:=ownerID(r);var req consumerRequest
    if decode(r,&req)!=nil||req.Name==""||req.Phone==""{errorJSON(w,400,"name and phone are required");return}
    plan,err:=normalizeMealPlan(req.MealPlan);if err!=nil{errorJSON(w,400,err.Error());return}
    if req.ConsumerID==""{req.ConsumerID="MM-"+strings.ToUpper(randomID()[:8])}
    if req.StartDate==""{errorJSON(w,400,"start_date is required");return}
    if req.EndDate==""{startTmp,err:=time.Parse("2006-01-02",req.StartDate);if err!=nil{errorJSON(w,400,"invalid start_date");return};req.EndDate=time.Date(startTmp.Year(),startTmp.Month()+1,0,0,0,0,0,startTmp.Location()).Format("2006-01-02")}
    start,err:=time.Parse("2006-01-02",req.StartDate);if err!=nil{errorJSON(w,400,"invalid start_date");return}
    end,err:=time.Parse("2006-01-02",req.EndDate);if err!=nil||end.Before(start){errorJSON(w,400,"invalid end_date");return}
    if req.Amount<=0{errorJSON(w,400,"amount must be greater than 0");return}
    if req.AmountPaid<0||req.AmountPaid>req.Amount{errorJSON(w,400,"amount paid must be between 0 and total amount");return}
    tx,err:=a.db.Begin();if err!=nil{errorJSON(w,500,"could not start transaction");return};defer tx.Rollback()
    var id,qr,sid uuid.UUID
    err=tx.QueryRow(`INSERT INTO consumers(owner_id,consumer_id,name,phone,meal_plan) VALUES($1,$2,$3,$4,$5) RETURNING id,qr_token`,oid,req.ConsumerID,req.Name,req.Phone,plan).Scan(&id,&qr)
    if err!=nil{errorJSON(w,409,"consumer ID already exists for this owner");return}
    status:=paymentStatus(req.Amount,req.AmountPaid); month:=start.Format("January 2006")
    err=tx.QueryRow(`INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,amount_paid,month_label,payment_status) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,id,start,end,req.Amount,req.AmountPaid,month,status).Scan(&sid)
    if err!=nil{errorJSON(w,500,"could not create subscription");return}
    if req.AmountPaid>0 { if _,err=tx.Exec(`INSERT INTO payments(consumer_id,subscription_id,amount,method) VALUES($1,$2,$3,'cash')`,id,sid,req.AmountPaid);err!=nil{errorJSON(w,500,"could not record initial payment");return} }
    if err=tx.Commit();err!=nil{errorJSON(w,500,"could not save consumer");return}
    writeJSON(w,201,map[string]any{"id":id,"consumer_id":req.ConsumerID,"qr_token":qr,"subscription_id":sid,"start_date":req.StartDate,"end_date":req.EndDate,"amount":req.Amount,"amount_paid":req.AmountPaid,"payment_status":status})
}

func (a *App) listConsumers(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);rows,err:=a.db.Query(`SELECT c.id,c.consumer_id,c.name,c.phone,c.meal_plan,c.active,c.qr_token,COALESCE(s.id,'00000000-0000-0000-0000-000000000000'),COALESCE(s.start_date,'0001-01-01'),COALESCE(s.end_date,'0001-01-01'),COALESCE(s.amount,0),COALESCE(NULLIF(s.monthly_amount,0),s.amount),COALESCE(s.amount_paid,0),COALESCE(s.month_label,''),COALESCE(s.payment_status,'pending') FROM consumers c LEFT JOIN LATERAL (SELECT * FROM subscriptions WHERE consumer_id=c.id ORDER BY end_date DESC LIMIT 1) s ON TRUE WHERE c.owner_id=$1 ORDER BY c.created_at DESC`,oid);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close()
    out:=[]map[string]any{};for rows.Next(){var id,qr,sid uuid.UUID;var cid,name,phone,plan,month,status string;var active bool;var start,end time.Time;var amount,monthly,paid float64;if err:=rows.Scan(&id,&cid,&name,&phone,&plan,&active,&qr,&sid,&start,&end,&amount,&monthly,&paid,&month,&status);err!=nil{continue};status=displayedPaymentStatus(paid,amount,end,time.Now());out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"meal_plan":plan,"active":active,"qr_token":qr,"subscription":map[string]any{"id":sid,"start_date":start.Format("2006-01-02"),"end_date":end.Format("2006-01-02"),"amount":amount,"monthly_amount":monthly,"amount_paid":paid,"month_label":month,"payment_status":status}})};writeJSON(w,200,out)
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
func (a *App) consumerQR(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return};var token uuid.UUID;var name,cid string;if err=a.db.QueryRow(`SELECT qr_token,name,consumer_id FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&token,&name,&cid);err!=nil{errorJSON(w,404,"consumer not found");return};png,err:=qrcode.Encode(token.String(),qrcode.Medium,256);if err!=nil{errorJSON(w,500,"could not generate QR");return};writeJSON(w,200,map[string]any{"consumer_id":cid,"name":name,"qr_token":token,"format":"png","image_base64":base64.StdEncoding.EncodeToString(png)})}

func (a *App) createSubscription(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);var req subscriptionRequest;if decode(r,&req)!=nil{errorJSON(w,400,"invalid request");return};cid,err:=uuid.Parse(req.ConsumerID);if err!=nil{errorJSON(w,400,"consumer_id must be a UUID");return};start,err:=time.Parse("2006-01-02",req.StartDate);if err!=nil{errorJSON(w,400,"invalid start_date");return};end,err:=time.Parse("2006-01-02",req.EndDate);if err!=nil||end.Before(start){errorJSON(w,400,"invalid end_date");return};if req.Amount<0||req.AmountPaid<0||req.AmountPaid>req.Amount{errorJSON(w,400,"amount paid must be between 0 and total amount");return};req.PaymentStatus=paymentStatus(req.Amount,req.AmountPaid);if req.MonthLabel==""{req.MonthLabel=start.Format("January 2006")}
    var sid uuid.UUID;err=a.db.QueryRow(`INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,amount_paid,month_label,payment_status) SELECT $1,$2,$3,$4,$5,$6,$7 WHERE EXISTS(SELECT 1 FROM consumers WHERE id=$1 AND owner_id=$8) RETURNING id`,cid,start,end,req.Amount,req.AmountPaid,req.MonthLabel,req.PaymentStatus,oid).Scan(&sid);if err!=nil{errorJSON(w,404,"consumer not found or subscription could not be created");return};if req.AmountPaid>0{_,_=a.db.Exec(`INSERT INTO payments(consumer_id,subscription_id,amount,method) VALUES($1,$2,$3,'cash')`,cid,sid,req.AmountPaid)};writeJSON(w,201,map[string]any{"id":sid,"consumer_id":cid,"start_date":req.StartDate,"end_date":req.EndDate,"amount":req.Amount,"amount_paid":req.AmountPaid,"month_label":req.MonthLabel,"payment_status":req.PaymentStatus})
}
func (a *App) expiringToday(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(`SELECT c.id,c.consumer_id,c.name,c.phone,s.amount,s.amount_paid,s.end_date FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.owner_id=$1 AND s.end_date=CURRENT_DATE ORDER BY c.name`,oid);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id uuid.UUID;var cid,name,phone string;var amount,paid float64;var end time.Time;if rows.Scan(&id,&cid,&name,&phone,&amount,&paid,&end)==nil{status:=displayedPaymentStatus(paid,amount,end,time.Now());out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"amount":amount,"amount_paid":paid,"payment_status":status,"reminder_eligible":status!="paid"})}};writeJSON(w,200,out)}
func (a *App) pendingPayments(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(`SELECT c.id,c.consumer_id,c.name,c.phone,s.end_date,s.amount,s.amount_paid FROM consumers c JOIN LATERAL (SELECT * FROM subscriptions WHERE consumer_id=c.id ORDER BY end_date DESC LIMIT 1) s ON TRUE WHERE c.owner_id=$1 AND s.amount_paid<s.amount ORDER BY s.end_date`,oid);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id uuid.UUID;var cid,name,phone string;var end time.Time;var amount,paid float64;if rows.Scan(&id,&cid,&name,&phone,&end,&amount,&paid)==nil{out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"end_date":end.Format("2006-01-02"),"amount":amount,"amount_paid":paid,"due":amount-paid,"payment_status":displayedPaymentStatus(paid,amount,end,time.Now())})}};writeJSON(w,200,out)}
func (a *App) recentPayments(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(`SELECT p.id,c.consumer_id,c.name,p.amount,p.method,COALESCE(p.reference,''),p.paid_at FROM payments p JOIN consumers c ON c.id=p.consumer_id WHERE c.owner_id=$1 ORDER BY p.paid_at DESC LIMIT 10`,oid);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id uuid.UUID;var cid,name,method,ref string;var amount float64;var paidAt time.Time;if rows.Scan(&id,&cid,&name,&amount,&method,&ref,&paidAt)==nil{out=append(out,map[string]any{"id":id,"consumer_id":cid,"name":name,"amount":amount,"method":method,"reference":ref,"paid_at":paidAt})}};writeJSON(w,200,out)}
func (a *App) createPayment(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);var req paymentRequest;if decode(r,&req)!=nil||req.Amount<=0{errorJSON(w,400,"consumer_id and positive amount are required");return};cid,err:=uuid.Parse(req.ConsumerID);if err!=nil{errorJSON(w,400,"invalid consumer_id");return};if req.Method==""{req.Method="cash"}
    var sid uuid.UUID;var total,paid float64;query:=`SELECT s.id,s.amount,s.amount_paid FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.consumer_id=$1 AND c.owner_id=$2 ORDER BY s.end_date DESC LIMIT 1`;err=a.db.QueryRow(query,cid,oid).Scan(&sid,&total,&paid);if err!=nil{errorJSON(w,404,"active subscription not found");return};if req.SubscriptionID!=""{sid,err=uuid.Parse(req.SubscriptionID);if err!=nil{errorJSON(w,400,"invalid subscription_id");return};err=a.db.QueryRow(`SELECT s.amount,s.amount_paid FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE s.id=$1 AND s.consumer_id=$2 AND c.owner_id=$3`,sid,cid,oid).Scan(&total,&paid);if err!=nil{errorJSON(w,404,"subscription not found");return}}
    if paid+req.Amount>total{errorJSON(w,400,fmt.Sprintf("payment exceeds remaining due of %.2f",total-paid));return}
    var pid uuid.UUID;err=a.db.QueryRow(`INSERT INTO payments(consumer_id,subscription_id,amount,method,reference) VALUES($1,$2,$3,$4,$5) RETURNING id`,cid,sid,req.Amount,req.Method,req.Reference).Scan(&pid);if err!=nil{errorJSON(w,500,"payment failed");return};newPaid:=paid+req.Amount;_,_=a.db.Exec(`UPDATE subscriptions SET amount_paid=$1,payment_status=$2 WHERE id=$3`,newPaid,paymentStatus(total,newPaid),sid);writeJSON(w,201,map[string]any{"id":pid,"consumer_id":cid,"subscription_id":sid,"amount":req.Amount,"amount_paid":newPaid,"due":total-newPaid,"method":req.Method})
}
func (a *App) scanAttendance(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);var req attendanceRequest;if decode(r,&req)!=nil{errorJSON(w,400,"invalid request");return};if req.Meal!="breakfast"&&req.Meal!="lunch"&&req.Meal!="dinner"{errorJSON(w,400,"meal must be breakfast, lunch or dinner");return};token,err:=uuid.Parse(req.QRToken);if err!=nil{errorJSON(w,400,"invalid qr_token");return};var cid uuid.UUID;var consumerID,name,plan string;var active bool;err=a.db.QueryRow(`SELECT c.id,c.consumer_id,c.name,c.meal_plan,c.active FROM consumers c WHERE c.qr_token=$1 AND c.owner_id=$2`,token,oid).Scan(&cid,&consumerID,&name,&plan,&active);if err!=nil{errorJSON(w,404,"consumer not found");return};if !active{errorJSON(w,403,"consumer is inactive");return};if plan!="all"&&!containsMeal(plan,req.Meal){errorJSON(w,403,"consumer is not subscribed to this meal");return}
    var end time.Time;err=a.db.QueryRow(`SELECT end_date FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1`,cid).Scan(&end);if err!=nil||end.Before(time.Now().Truncate(24*time.Hour)){errorJSON(w,403,"consumer subscription is not active");return}
    var aid uuid.UUID;err=a.db.QueryRow(`INSERT INTO attendance(consumer_id,attendance_date,meal) VALUES($1,CURRENT_DATE,$2) ON CONFLICT(consumer_id,attendance_date,meal) DO NOTHING RETURNING id`,cid,req.Meal).Scan(&aid);if err!=nil{errorJSON(w,409,"attendance already marked for this meal");return};writeJSON(w,201,map[string]any{"attendance_id":aid,"consumer_id":consumerID,"name":name,"meal":req.Meal,"date":time.Now().Format("2006-01-02"),"status":"marked"})
}
func containsMeal(plan,meal string) bool { for _,m:=range strings.Split(plan,","){if m==meal{return true}};return false }
func (a *App) todayAttendance(w http.ResponseWriter,r *http.Request){oid,_:=ownerID(r);rows,err:=a.db.Query(`SELECT c.consumer_id,c.name,at.meal,at.scanned_at FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id=$1 AND at.attendance_date=CURRENT_DATE ORDER BY at.scanned_at DESC`,oid);if err!=nil{errorJSON(w,500,"database error");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var cid,name,meal string;var scanned time.Time;if rows.Scan(&cid,&name,&meal,&scanned)==nil{out=append(out,map[string]any{"consumer_id":cid,"name":name,"meal":meal,"scanned_at":scanned})}};writeJSON(w,200,out)}
func logging(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){start:=time.Now();next.ServeHTTP(w,r);log.Printf("%s %s %s",r.Method,r.URL.Path,time.Since(start))})}
func cors(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Access-Control-Allow-Origin","*");w.Header().Set("Access-Control-Allow-Headers","Authorization, Content-Type");w.Header().Set("Access-Control-Allow-Methods","GET, POST, PATCH, DELETE, OPTIONS");if r.Method==http.MethodOptions{w.WriteHeader(http.StatusNoContent);return};next.ServeHTTP(w,r)})}
var _ = fmt.Sprintf
