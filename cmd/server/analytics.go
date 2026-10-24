package main

import (
    "errors"
    "log"
    "net/http"
    "time"

    "github.com/google/uuid"
)

var indiaLocation = time.FixedZone("IST", 5*60*60+30*60)

func parseMonth(value string) (time.Time, time.Time, error) {
    if value == "" {
        value = time.Now().In(indiaLocation).Format("2006-01")
    }
    start, err := time.ParseInLocation("2006-01", value, indiaLocation)
    if err != nil {
        return time.Time{}, time.Time{}, errors.New("month must use YYYY-MM format")
    }
    return start, start.AddDate(0, 1, 0).Add(-24 * time.Hour), nil
}
func dateBeforeInIndia(date, now time.Time) bool {
    year, month, day := date.Date()
    todayYear, todayMonth, todayDay := now.In(indiaLocation).Date()
    return year < todayYear ||
        (year == todayYear && (month < todayMonth ||
            (month == todayMonth && day < todayDay)))
}

// A subscription becomes overdue when its coverage has ended, even if the
// previous period was fully paid. This represents an overdue renewal.
func displayedPaymentStatus(paid, amount float64, endDate, asOf time.Time) string {
    if endDate.IsZero() || amount <= 0 {
        return "pending"
    }
    if dateBeforeInIndia(endDate, asOf) {
        return "overdue"
    }
    if paid >= amount {
        return "paid"
    }
    if paid > 0 {
        return "partial"
    }
    return "pending"
}
func (a *App) monthlyAnalytics(w http.ResponseWriter, r *http.Request) {
    oid, _ := ownerID(r)
    start, end, err := parseMonth(r.URL.Query().Get("month"))
    if err != nil { errorJSON(w,400,err.Error()); return }

    // Revenue is recognized when the payment was actually recorded,
    // not when the subscription period started.
    var earnings float64
    if err := a.db.QueryRow(`
        SELECT COALESCE(SUM(p.amount),0)
        FROM payments p
        JOIN consumers c ON c.id=p.consumer_id
        WHERE c.owner_id=$1 AND p.paid_at >= $2 AND p.paid_at < $3`,
        oid, start, start.AddDate(0, 1, 0)).Scan(&earnings); err != nil {
        log.Printf("analytics monthly collections query failed: %v", err)
        errorJSON(w, 500, "could not calculate monthly collections")
        return
    }

    var newConsumers int
    if err := a.db.QueryRow(`SELECT COUNT(*) FROM consumers c WHERE c.owner_id=$1 AND c.created_at >= $2 AND c.created_at < $3`, oid, start, start.AddDate(0, 1, 0)).Scan(&newConsumers); err != nil {
        log.Printf("analytics new consumers query failed: %v", err)
        errorJSON(w, 500, "could not calculate new consumers")
        return
    }

    // Treat the selected month's status counts as a snapshot at month-end,
    // capped at today for the current month. Do not filter out subscriptions
    // that expired before the month began: those are overdue, not nonexistent.
    asOf := end.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
    today := time.Now().In(indiaLocation)
    if asOf.After(today) { asOf = today }
    rows, err := a.db.Query(`SELECT DISTINCT ON (s.consumer_id) s.amount,s.amount_paid,s.end_date
        FROM subscriptions s
        JOIN consumers c ON c.id=s.consumer_id
        WHERE c.owner_id=$1
        ORDER BY s.consumer_id,s.end_date DESC,s.created_at DESC`, oid)
    if err != nil { log.Printf("analytics subscription status query failed: %v", err); errorJSON(w,500,"database error"); return }
    defer rows.Close()
    paidCount,partialCount,pendingCount,overdueCount:=0,0,0,0
    for rows.Next() {
        var amount, paid float64
        var endDate time.Time
        if err := rows.Scan(&amount, &paid, &endDate); err != nil {
            errorJSON(w, 500, "could not read subscription status data")
            return
        }
        switch displayedPaymentStatus(paid, amount, endDate, asOf) {
        case "paid":
            paidCount++
        case "partial":
            partialCount++
        case "pending":
            pendingCount++
        case "overdue":
            overdueCount++
        }
    }
    if err := rows.Err(); err != nil {
        errorJSON(w, 500, "could not read subscription status data")
        return
    }

    type point struct { Month string `json:"month"`; Label string `json:"label"`; Earnings float64 `json:"earnings"` }
    trendRows,err:=a.db.Query(`
        WITH months AS (
            SELECT m
            FROM generate_series(date_trunc('month',$1::date)-interval '11 months',date_trunc('month',$1::date),interval '1 month') m
        ), payment_revenue AS (
            SELECT p.amount,p.paid_at AT TIME ZONE 'Asia/Kolkata' AS revenue_at
            FROM payments p
            JOIN consumers c ON c.id=p.consumer_id AND c.owner_id=$2
        )
        SELECT to_char(m.m,'YYYY-MM'),to_char(m.m,'Mon'),COALESCE(SUM(pr.amount),0)
        FROM months m
        LEFT JOIN payment_revenue pr ON pr.revenue_at>=m.m AND pr.revenue_at<m.m+interval '1 month'
        GROUP BY m.m ORDER BY m.m`,start.Format("2006-01-02"),oid)
    if err!=nil{log.Printf("analytics trend query failed: %v",err);errorJSON(w,500,"database error");return};defer trendRows.Close()
    trend := []point{}
    for trendRows.Next() {
        var p point
        if err := trendRows.Scan(&p.Month, &p.Label, &p.Earnings); err != nil {
            errorJSON(w, 500, "could not read monthly collections trend")
            return
        }
        trend = append(trend, p)
    }
    if err := trendRows.Err(); err != nil {
        errorJSON(w, 500, "could not read monthly collections trend")
        return
    }

    attendance:=map[string]int{"breakfast":0,"lunch":0,"dinner":0}
    ar, err := a.db.Query(`SELECT at.meal,COUNT(*) FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id=$1 AND at.attendance_date >= $2 AND at.attendance_date < $3 GROUP BY at.meal`, oid, start.Format("2006-01-02"), start.AddDate(0, 1, 0).Format("2006-01-02"))
    if err != nil {
        log.Printf("analytics attendance query failed: %v", err)
        errorJSON(w, 500, "could not calculate attendance analytics")
        return
    }
    defer ar.Close()
    for ar.Next() {
        var meal string
        var count int
        if err := ar.Scan(&meal, &count); err != nil {
            errorJSON(w, 500, "could not read attendance analytics")
            return
        }
        attendance[meal] = count
    }
    if err := ar.Err(); err != nil {
        errorJSON(w, 500, "could not read attendance analytics")
        return
    }

    writeJSON(w,200,map[string]any{"month":start.Format("2006-01"),"month_label":start.Format("January 2006"),"earnings":earnings,"new_consumers":newConsumers,"paid":paidCount,"partial":partialCount,"pending":pendingCount,"overdue":overdueCount,"attendance":attendance,"earnings_trend":trend})
}

func (a *App) consumerProfile(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return}
    monthStart,monthEnd,err:=parseMonth(r.URL.Query().Get("month"));if err!=nil{errorJSON(w,400,err.Error());return}
    var cid,name,phone,plan string;var active bool;var qr uuid.UUID
    if err=a.db.QueryRow(`SELECT consumer_id,name,phone,meal_plan,active,qr_token FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&cid,&name,&phone,&plan,&active,&qr);err!=nil{errorJSON(w,404,"consumer not found");return}

    type sub struct{ID uuid.UUID;Start,End time.Time;Amount,Paid float64}
    sr,err:=a.db.Query(`SELECT id,start_date,end_date,amount,amount_paid FROM subscriptions WHERE consumer_id=$1 AND start_date <= $2 AND end_date >= $3 ORDER BY end_date DESC`,id,monthEnd.Format("2006-01-02"),monthStart.Format("2006-01-02"));if err!=nil{errorJSON(w,500,"database error");return};defer sr.Close()
    subscriptions:=[]sub{};for sr.Next(){var s sub;if sr.Scan(&s.ID,&s.Start,&s.End,&s.Amount,&s.Paid)==nil{subscriptions=append(subscriptions,s)}}

    // The profile's financial status always reflects the latest subscription.
    var latest sub
    _=a.db.QueryRow(`SELECT id,start_date,end_date,amount,amount_paid FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1`,id).Scan(&latest.ID,&latest.Start,&latest.End,&latest.Amount,&latest.Paid)

    attendance:=map[string]map[string]bool{"breakfast":{},"lunch":{},"dinner":{}}
    ar,err:=a.db.Query(`SELECT attendance_date,meal FROM attendance WHERE consumer_id=$1 AND attendance_date >= $2 AND attendance_date < $3`,id,monthStart.Format("2006-01-02"),monthStart.AddDate(0,1,0).Format("2006-01-02"));if err==nil{defer ar.Close();for ar.Next(){var d time.Time;var meal string;if ar.Scan(&d,&meal)==nil{if _,ok:=attendance[meal];ok{attendance[meal][d.Format("2006-01-02")]=true}}}}

    expected:=map[string]int{"breakfast":0,"lunch":0,"dinner":0};present:=map[string]int{"breakfast":0,"lunch":0,"dinner":0};days:=[]map[string]any{}
    for d:=monthStart;d.Before(monthStart.AddDate(0,1,0));d=d.AddDate(0,0,1){day:=map[string]any{"date":d.Format("2006-01-02"),"breakfast":false,"lunch":false,"dinner":false};for _,s:=range subscriptions{if d.Before(s.Start)||d.After(s.End){continue};for _,m:=range []string{"breakfast","lunch","dinner"}{if plan=="all"||containsMeal(plan,m){expected[m]++;if attendance[m][d.Format("2006-01-02")]{day[m]=true}}}};for _,m:=range []string{"breakfast","lunch","dinner"}{if day[m].(bool){present[m]++}};days=append(days,day)}
    type paymentHistory struct{ID uuid.UUID `json:"id"`;Amount float64 `json:"amount"`;Method string `json:"method"`;Reference string `json:"reference"`;PaidAt time.Time `json:"paid_at"`;SubStart time.Time `json:"subscription_start,omitempty"`;SubEnd time.Time `json:"subscription_end,omitempty"`}
    pr,err:=a.db.Query(`SELECT p.id,p.amount,COALESCE(p.method,''),COALESCE(p.reference,''),p.paid_at,s.start_date,s.end_date FROM payments p LEFT JOIN subscriptions s ON s.id=p.subscription_id WHERE p.consumer_id=$1 ORDER BY p.paid_at DESC,p.id DESC`,id)
    if err!=nil{errorJSON(w,500,"database error");return}
    defer pr.Close()
    payments:=[]paymentHistory{}
    for pr.Next(){var p paymentHistory;if pr.Scan(&p.ID,&p.Amount,&p.Method,&p.Reference,&p.PaidAt,&p.SubStart,&p.SubEnd)==nil{payments=append(payments,p)}}
    status:=displayedPaymentStatus(latest.Paid,latest.Amount,latest.End,time.Now())
    writeJSON(w,200,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"meal_plan":plan,"active":active,"qr_token":qr,"subscription":map[string]any{"id":latest.ID,"start_date":formatDate(latest.Start),"end_date":formatDate(latest.End),"amount":latest.Amount,"amount_paid":latest.Paid,"due":latest.Amount-latest.Paid,"payment_status":status},"month":monthStart.Format("2006-01"),"month_label":monthStart.Format("January 2006"),"attendance":map[string]any{"present":present,"expected":expected,"days":days},"payments":payments})
}

func formatDate(t time.Time)string{if t.IsZero(){return ""};return t.Format("2006-01-02")}
