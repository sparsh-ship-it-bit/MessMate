package main

import (
    "errors"
    "net/http"
    "time"

    "github.com/google/uuid"
)

func parseMonth(value string) (time.Time, time.Time, error) {
    if value == "" { value = time.Now().Format("2006-01") }
    start, err := time.Parse("2006-01", value)
    if err != nil { return time.Time{}, time.Time{}, errors.New("month must use YYYY-MM format") }
    return start, start.AddDate(0, 1, 0).Add(-24*time.Hour), nil
}

// A subscription becomes overdue when its coverage has ended, even if the
// previous period was fully paid. This represents an overdue renewal.
func displayedPaymentStatus(paid, amount float64, endDate, asOf time.Time) string {
    if !endDate.IsZero() && endDate.Before(asOf) { return "overdue" }
    if paid >= amount { return "paid" }
    if paid > 0 { return "partial" }
    return "pending"
}

func (a *App) monthlyAnalytics(w http.ResponseWriter, r *http.Request) {
    oid, _ := ownerID(r)
    start, end, err := parseMonth(r.URL.Query().Get("month"))
    if err != nil { errorJSON(w,400,err.Error()); return }

    var earnings float64
    _ = a.db.QueryRow(`SELECT COALESCE(SUM(p.amount),0) FROM payments p JOIN consumers c ON c.id=p.consumer_id WHERE c.owner_id=$1 AND p.paid_at >= $2 AND p.paid_at < $3`, oid,start,start.AddDate(0,1,0)).Scan(&earnings)

    var newConsumers int
    _ = a.db.QueryRow(`SELECT COUNT(DISTINCT s.consumer_id) FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE c.owner_id=$1 AND s.start_date >= $2 AND s.start_date < $3`, oid,start,start.AddDate(0,1,0)).Scan(&newConsumers)

    rows, err := a.db.Query(`SELECT DISTINCT ON (s.consumer_id) s.amount,s.amount_paid,s.end_date FROM subscriptions s JOIN consumers c ON c.id=s.consumer_id WHERE c.owner_id=$1 AND s.start_date <= $2 AND s.end_date >= $3 ORDER BY s.consumer_id,s.end_date DESC`, oid,end,start)
    if err != nil { errorJSON(w,500,"database error"); return }
    defer rows.Close()
    paidCount,partialCount,pendingCount,overdueCount:=0,0,0,0
    for rows.Next(){var amount,paid float64;var endDate time.Time;if rows.Scan(&amount,&paid,&endDate)!=nil{continue};switch displayedPaymentStatus(paid,amount,endDate,end){case "paid":paidCount++;case "partial":partialCount++;case "pending":pendingCount++;case "overdue":overdueCount++}}

    type point struct { Month string `json:"month"`; Label string `json:"label"`; Earnings float64 `json:"earnings"` }
    trendRows,err:=a.db.Query(`SELECT to_char(m,'YYYY-MM'),to_char(m,'Mon'),COALESCE(SUM(p.amount),0) FROM generate_series(date_trunc('month',$1::date)-interval '11 months',date_trunc('month',$1::date),interval '1 month') m LEFT JOIN (payments p JOIN consumers c ON c.id=p.consumer_id AND c.owner_id=$2) ON p.paid_at>=m AND p.paid_at<m+interval '1 month' GROUP BY m ORDER BY m`,start,oid)
    if err!=nil{errorJSON(w,500,"database error");return};defer trendRows.Close()
    trend:=[]point{};for trendRows.Next(){var p point;if trendRows.Scan(&p.Month,&p.Label,&p.Earnings)==nil{trend=append(trend,p)}}

    attendance:=map[string]int{"breakfast":0,"lunch":0,"dinner":0}
    ar,err:=a.db.Query(`SELECT at.meal,COUNT(*) FROM attendance at JOIN consumers c ON c.id=at.consumer_id WHERE c.owner_id=$1 AND at.attendance_date >= $2 AND at.attendance_date < $3 GROUP BY at.meal`,oid,start,start.AddDate(0,1,0))
    if err==nil{defer ar.Close();for ar.Next(){var meal string;var count int;if ar.Scan(&meal,&count)==nil{attendance[meal]=count}}}

    writeJSON(w,200,map[string]any{"month":start.Format("2006-01"),"month_label":start.Format("January 2006"),"earnings":earnings,"new_consumers":newConsumers,"paid":paidCount,"partial":partialCount,"pending":pendingCount,"overdue":overdueCount,"attendance":attendance,"earnings_trend":trend})
}

func (a *App) consumerProfile(w http.ResponseWriter,r *http.Request){
    oid,_:=ownerID(r);id,err:=uuid.Parse(r.PathValue("id"));if err!=nil{errorJSON(w,400,"invalid consumer id");return}
    monthStart,monthEnd,err:=parseMonth(r.URL.Query().Get("month"));if err!=nil{errorJSON(w,400,err.Error());return}
    var cid,name,phone,plan string;var active bool;var qr uuid.UUID
    if err=a.db.QueryRow(`SELECT consumer_id,name,phone,meal_plan,active,qr_token FROM consumers WHERE id=$1 AND owner_id=$2`,id,oid).Scan(&cid,&name,&phone,&plan,&active,&qr);err!=nil{errorJSON(w,404,"consumer not found");return}

    type sub struct{ID uuid.UUID;Start,End time.Time;Amount,Paid float64}
    sr,err:=a.db.Query(`SELECT id,start_date,end_date,amount,amount_paid FROM subscriptions WHERE consumer_id=$1 AND start_date <= $2 AND end_date >= $3 ORDER BY end_date DESC`,id,monthEnd,monthStart);if err!=nil{errorJSON(w,500,"database error");return};defer sr.Close()
    subscriptions:=[]sub{};for sr.Next(){var s sub;if sr.Scan(&s.ID,&s.Start,&s.End,&s.Amount,&s.Paid)==nil{subscriptions=append(subscriptions,s)}}

    // The profile's financial status always reflects the latest subscription.
    var latest sub
    _=a.db.QueryRow(`SELECT id,start_date,end_date,amount,amount_paid FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1`,id).Scan(&latest.ID,&latest.Start,&latest.End,&latest.Amount,&latest.Paid)

    attendance:=map[string]map[string]bool{"breakfast":{},"lunch":{},"dinner":{}}
    ar,err:=a.db.Query(`SELECT attendance_date,meal FROM attendance WHERE consumer_id=$1 AND attendance_date >= $2 AND attendance_date < $3`,id,monthStart,monthStart.AddDate(0,1,0));if err==nil{defer ar.Close();for ar.Next(){var d time.Time;var meal string;if ar.Scan(&d,&meal)==nil{if _,ok:=attendance[meal];ok{attendance[meal][d.Format("2006-01-02")]=true}}}}

    expected:=map[string]int{"breakfast":0,"lunch":0,"dinner":0};present:=map[string]int{"breakfast":0,"lunch":0,"dinner":0};days:=[]map[string]any{}
    for d:=monthStart;d.Before(monthStart.AddDate(0,1,0));d=d.AddDate(0,0,1){day:=map[string]any{"date":d.Format("2006-01-02"),"breakfast":false,"lunch":false,"dinner":false};for _,s:=range subscriptions{if d.Before(s.Start)||d.After(s.End){continue};for _,m:=range []string{"breakfast","lunch","dinner"}{if plan=="all"||containsMeal(plan,m){expected[m]++;if attendance[m][d.Format("2006-01-02")]{day[m]=true}}}};for _,m:=range []string{"breakfast","lunch","dinner"}{if day[m].(bool){present[m]++}};days=append(days,day)}
    status:=displayedPaymentStatus(latest.Paid,latest.Amount,latest.End,time.Now())
    writeJSON(w,200,map[string]any{"id":id,"consumer_id":cid,"name":name,"phone":phone,"meal_plan":plan,"active":active,"qr_token":qr,"subscription":map[string]any{"id":latest.ID,"start_date":formatDate(latest.Start),"end_date":formatDate(latest.End),"amount":latest.Amount,"amount_paid":latest.Paid,"due":latest.Amount-latest.Paid,"payment_status":status},"month":monthStart.Format("2006-01"),"month_label":monthStart.Format("January 2006"),"attendance":map[string]any{"present":present,"expected":expected,"days":days}})
}

func formatDate(t time.Time)string{if t.IsZero(){return ""};return t.Format("2006-01-02")}
