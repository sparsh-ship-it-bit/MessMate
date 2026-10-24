package main

import (
  "crypto/rand"
  "crypto/sha256"
  "encoding/hex"
  "fmt"
  "net/http"
  "strings"
  "time"

  "github.com/google/uuid"
)

type pairingCodeRequest struct{}
type pairingExchangeRequest struct { Code string `json:"code"` }
type mobileScanRequest struct { Value string `json:"value"`; Meal string `json:"meal"` }

func hashPairSecret(s string) string { h:=sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func securePairSecret(n int) (string,error) { b:=make([]byte,n); if _,err:=rand.Read(b);err!=nil{return "",err}; return hex.EncodeToString(b),nil }

func (a *App) createMobilePairing(w http.ResponseWriter,r *http.Request) {
  oid,err:=ownerID(r); if err!=nil { errorJSON(w,401,"authentication required"); return }
  code,err:=securePairSecret(3); if err!=nil { errorJSON(w,500,"could not create pairing code"); return }
  code=fmt.Sprintf("%06d", (int(code[0])<<16|int(code[1])<<8|int(code[2]))%1000000)
  expires:=time.Now().Add(5*time.Minute)
  _,err=a.db.Exec(`INSERT INTO mobile_pairings(owner_id,code_hash,expires_at) VALUES($1,$2,$3)`,oid,hashPairSecret(code),expires)
  if err!=nil { errorJSON(w,500,"could not create pairing code"); return }
  writeJSON(w,201,map[string]any{"code":code,"expires_at":expires,"mobile_url":"/mobile-scan.html"})
}

func (a *App) exchangeMobilePairing(w http.ResponseWriter,r *http.Request) {
  var req pairingExchangeRequest
  req.Code=strings.TrimSpace(req.Code)
  if decode(r,&req)!=nil || len(strings.TrimSpace(req.Code))!=6 { errorJSON(w,400,"enter the six-digit pairing code"); return }
  req.Code=strings.TrimSpace(req.Code)
  token,err:=securePairSecret(32); if err!=nil { errorJSON(w,500,"could not pair device"); return }
  var pairingID,owner uuid.UUID
  err=a.db.QueryRow(`UPDATE mobile_pairings SET device_token_hash=$1,paired_at=NOW(),code_hash=NULL WHERE code_hash=$2 AND expires_at>NOW() AND paired_at IS NULL RETURNING id,owner_id`,hashPairSecret(token),hashPairSecret(req.Code)).Scan(&pairingID,&owner)
  if err!=nil { errorJSON(w,401,"pairing code is invalid, expired, or already used"); return }
  writeJSON(w,200,map[string]any{"device_token":token,"pairing_id":pairingID,"owner_id":owner,"owner_name":"MessMate","expires_in_days":30})
}

func (a *App) mobileScan(w http.ResponseWriter,r *http.Request) {
  token:=strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer "))
  if token=="" { errorJSON(w,401,"device is not paired"); return }
  var owner uuid.UUID
  var pairing uuid.UUID
  err:=a.db.QueryRow(`SELECT id,owner_id FROM mobile_pairings WHERE device_token_hash=$1 AND paired_at IS NOT NULL AND revoked_at IS NULL AND paired_at>NOW()-INTERVAL '30 days' ORDER BY paired_at DESC LIMIT 1`,hashPairSecret(token)).Scan(&pairing,&owner)
  if err!=nil { errorJSON(w,401,"device pairing expired or disconnected; pair again from desktop"); return }
  var req mobileScanRequest
  if decode(r,&req)!=nil || strings.TrimSpace(req.Value)=="" { errorJSON(w,400,"scan a consumer QR code or enter a consumer ID"); return }
  value:=strings.TrimSpace(req.Value)
  meal:=strings.ToLower(strings.TrimSpace(req.Meal))
  if meal!="breakfast" && meal!="lunch" && meal!="dinner" { errorJSON(w,400,"select breakfast, lunch or dinner before scanning"); return }
  var id,qrToken uuid.UUID
  var consumerID,name,phone,plan string
  var active bool
  // Accept the QR token used by MessMate consumer QR codes, or the human-readable consumer ID.
  err=a.db.QueryRow(`SELECT id,qr_token,consumer_id,name,phone,meal_plan,active FROM consumers WHERE owner_id=$1 AND (qr_token::text=$2 OR lower(consumer_id)=lower($2)) LIMIT 1`,owner,value).Scan(&id,&qrToken,&consumerID,&name,&phone,&plan,&active)
  if err!=nil { errorJSON(w,404,"consumer not found in this MessMate account"); return }
  if !active { errorJSON(w,403,"consumer is inactive"); return }
  if plan!="all" && !containsMeal(plan,meal) { errorJSON(w,403,"consumer is not subscribed to this meal"); return }
  var end time.Time
  err=a.db.QueryRow(`SELECT end_date FROM subscriptions WHERE consumer_id=$1 ORDER BY end_date DESC LIMIT 1`,id).Scan(&end)
  if err!=nil || dateBeforeInIndia(end,time.Now()) { errorJSON(w,403,"consumer subscription is not active"); return }
  var attendanceID uuid.UUID
  err=a.db.QueryRow(`INSERT INTO attendance(consumer_id,attendance_date,meal) VALUES($1,(NOW() AT TIME ZONE 'Asia/Kolkata')::date,$2) ON CONFLICT(consumer_id,attendance_date,meal) DO NOTHING RETURNING id`,id,meal).Scan(&attendanceID)
  if err!=nil { errorJSON(w,409,"attendance already marked for this meal today"); return }
  var scanID int64
  err=a.db.QueryRow(`INSERT INTO mobile_scan_events(owner_id,pairing_id,consumer_id,consumer_code,consumer_name,active) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,owner,pairing,id,consumerID,name,active).Scan(&scanID)
  if err!=nil { errorJSON(w,500,"attendance was marked but could not sync the scan to desktop"); return }
  writeJSON(w,200,map[string]any{"scan_id":scanID,"attendance_id":attendanceID,"consumer_id":consumerID,"name":name,"phone":phone,"meal_plan":plan,"active":active,"meal":meal,"message":"Attendance marked and synced to desktop"})
}

func (a *App) mobileScanEvents(w http.ResponseWriter,r *http.Request) {
  oid,err:=ownerID(r); if err!=nil { errorJSON(w,401,"authentication required"); return }
  var after int64
  if v:=r.URL.Query().Get("after");v!="" { if _,e:=fmt.Sscanf(v,"%d",&after);e!=nil {errorJSON(w,400,"invalid cursor");return} }
  var connected bool
  if err:=a.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM mobile_pairings WHERE owner_id=$1 AND paired_at IS NOT NULL AND revoked_at IS NULL AND last_seen_at>NOW()-INTERVAL '35 seconds')`,oid).Scan(&connected); err!=nil { errorJSON(w,500,"could not check mobile connection"); return }
  rows,err:=a.db.Query(`SELECT id,consumer_code,consumer_name,active,created_at FROM mobile_scan_events WHERE owner_id=$1 AND id>$2 ORDER BY id ASC LIMIT 30`,oid,after)
  if err!=nil { errorJSON(w,500,"could not fetch mobile scans"); return }
  defer rows.Close()
  events:=[]map[string]any{}
  for rows.Next(){var id int64;var code,name string;var active bool;var at time.Time;if rows.Scan(&id,&code,&name,&active,&at)==nil{events=append(events,map[string]any{"id":id,"consumer_id":code,"name":name,"active":active,"scanned_at":at})}}
  writeJSON(w,200,map[string]any{"events":events,"connected":connected})
}

func (a *App) mobileHeartbeat(w http.ResponseWriter, r *http.Request) {
  token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
  if token == "" { errorJSON(w, 401, "device is not paired"); return }
  result, err := a.db.Exec(`UPDATE mobile_pairings SET last_seen_at=NOW() WHERE device_token_hash=$1 AND paired_at IS NOT NULL AND revoked_at IS NULL AND paired_at>NOW()-INTERVAL '30 days'`, hashPairSecret(token))
  if err != nil { errorJSON(w, 500, "could not update phone connection"); return }
  n, err := result.RowsAffected()
  if err != nil || n == 0 { errorJSON(w, 401, "device pairing expired or disconnected; pair again from desktop"); return }
  writeJSON(w, 200, map[string]any{"connected": true})
}
