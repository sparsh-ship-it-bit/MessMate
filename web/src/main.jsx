import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Html5Qrcode } from 'html5-qrcode';
import './styles.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080';

async function api(path, options = {}) {
  const token = localStorage.getItem('messmate_token');
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${API}${path}`, { ...options, headers });
  const text = await res.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
  if (!res.ok) throw new Error(data.error || `Request failed (${res.status})`);
  return data;
}

function Auth({ onLogin }) {
  const [mode, setMode] = useState('login');
  const [form, setForm] = useState({ name:'', email:'', phone:'', password:'' });
  const [error, setError] = useState('');
  const submit = async e => {
    e.preventDefault(); setError('');
    try {
      const data = await api(`/api/v1/auth/${mode}`, { method:'POST', body:JSON.stringify(form) });
      localStorage.setItem('messmate_token', data.token); onLogin();
    } catch (err) { setError(err.message); }
  };
  return <main className="auth-shell"><section className="auth-card">
    <div className="brand"><span className="logo">M</span><div><b>MessMate</b><small>Mess management, simplified.</small></div></div>
    <h1>{mode === 'login' ? 'Welcome back' : 'Create owner account'}</h1>
    <p className="muted">Manage consumers, subscriptions, payments and meals from one place.</p>
    <form onSubmit={submit}>
      {mode === 'register' && <><label>Name<input required value={form.name} onChange={e=>setForm({...form,name:e.target.value})} placeholder="Mess owner name" /></label><label>Phone<input value={form.phone} onChange={e=>setForm({...form,phone:e.target.value})} placeholder="9876543210" /></label></>}
      <label>Email<input required type="email" value={form.email} onChange={e=>setForm({...form,email:e.target.value})} placeholder="owner@example.com" /></label>
      <label>Password<input required type="password" minLength="8" value={form.password} onChange={e=>setForm({...form,password:e.target.value})} placeholder="At least 8 characters" /></label>
      {error && <div className="error">{error}</div>}
      <button className="primary full">{mode === 'login' ? 'Sign in' : 'Create account'}</button>
    </form>
    <button className="link" onClick={()=>{setMode(mode==='login'?'register':'login');setError('')}}>{mode==='login' ? 'New to MessMate? Create an account' : 'Already have an account? Sign in'}</button>
  </section></main>
}

function Dashboard({ onLogout }) {
  const [stats,setStats]=useState(null), [consumers,setConsumers]=useState([]), [pending,setPending]=useState([]), [attendance,setAttendance]=useState([]);
  const [tab,setTab]=useState('dashboard'), [showAdd,setShowAdd]=useState(false), [notice,setNotice]=useState('');
  const [form,setForm]=useState({name:'',phone:'',consumer_id:'',meal_plan:'all'});
  const [qr,setQr]=useState(null), [scan,setScan]=useState(''), [meal,setMeal]=useState('lunch'), [scanner,setScanner]=useState(null);

  const load=async()=>{ try { const [s,c,p,a]=await Promise.all([api('/api/v1/dashboard'),api('/api/v1/consumers'),api('/api/v1/payments/pending'),api('/api/v1/attendance/today')]); setStats(s);setConsumers(c);setPending(p);setAttendance(a); } catch(e){setNotice(e.message)} };
  useEffect(()=>{load()},[]);
  const active=useMemo(()=>consumers.filter(c=>c.active),[consumers]);

  const addConsumer=async e=>{e.preventDefault();try{await api('/api/v1/consumers',{method:'POST',body:JSON.stringify(form)});setForm({name:'',phone:'',consumer_id:'',meal_plan:'all'});setShowAdd(false);setNotice('Consumer added successfully');load()}catch(e){setNotice(e.message)}};
  const showQR=async c=>{try{setQr(await api(`/api/v1/consumers/${c.id}/qr`))}catch(e){setNotice(e.message)}};
  const markAttendance=async token=>{try{await api('/api/v1/attendance/scan',{method:'POST',body:JSON.stringify({qr_token:token,meal})});setNotice('Attendance marked');setScan('');load()}catch(e){setNotice(e.message)}};
  const startScanner=async()=>{
    const reader='messmate-reader'; const q=new Html5Qrcode(reader); setScanner(q);
    try { await q.start({facingMode:'environment'},{fps:10,qrbox:{width:240,height:240}},text=>{q.stop().catch(()=>{});setScanner(null);markAttendance(text)},()=>{}); }
    catch(e){setNotice('Camera could not start. Check browser camera permission.');setScanner(null)}
  };
  const stopScanner=()=>{if(scanner) scanner.stop().catch(()=>{});setScanner(null)};

  return <div className="app"><aside><div className="brand side"><span className="logo">M</span><div><b>MessMate</b><small>Owner console</small></div></div><nav>{[['dashboard','Overview'],['consumers','Consumers'],['attendance','Attendance'],['payments','Payments']].map(([id,label])=><button className={tab===id?'nav active':'nav'} onClick={()=>setTab(id)} key={id}>{label}</button>)}</nav><button className="logout" onClick={()=>{localStorage.removeItem('messmate_token');onLogout()}}>Sign out</button></aside>
  <section className="content"><header><div><p className="eyebrow">MESS MANAGEMENT</p><h2>{tab==='dashboard'?'Good morning 👋':tab[0].toUpperCase()+tab.slice(1)}</h2></div><button className="primary" onClick={()=>setShowAdd(true)}>+ Add consumer</button></header>
  {notice && <div className="notice" onClick={()=>setNotice('')}>{notice}</div>}
  {tab==='dashboard' && <><div className="stats">{[['Active consumers',stats?.active_consumers??'—'],['Expiring today',stats?.expiring_today??'—'],['Pending payments',stats?.pending_payments??'—'],['Today collections',stats?.today_collections!=null?`₹${stats.today_collections}`:'—'],['Today attendance',stats?.today_attendance??'—']].map(([l,v])=><div className="stat" key={l}><span>{l}</span><strong>{v}</strong></div>)}</div><div className="grid"><section className="panel"><div className="panel-head"><h3>Recent consumers</h3><button className="link" onClick={()=>setTab('consumers')}>View all</button></div>{consumers.slice(0,6).map(c=><div className="row" key={c.id}><div className="avatar">{c.name?.[0]?.toUpperCase()}</div><div className="grow"><b>{c.name}</b><small>{c.consumer_id} · {c.meal_plan}</small></div><span className={c.active?'badge ok':'badge'}>{c.active?'Active':'Inactive'}</span><button className="ghost" onClick={()=>showQR(c)}>QR</button></div>)}{!consumers.length&&<Empty text="No consumers yet."/>}</section><section className="panel"><div className="panel-head"><h3>Today's attendance</h3><button className="primary small" onClick={()=>setTab('attendance')}>Scan</button></div>{attendance.slice(0,7).map((a,i)=><div className="row" key={a.id||i}><div className="grow"><b>{a.name||a.consumer_name||a.consumer_id}</b><small>{a.meal} · {a.attendance_time||a.created_at||''}</small></div><span className="badge ok">Present</span></div>)}{!attendance.length&&<Empty text="No attendance marked today."/>}</section></div></>}
  {tab==='consumers' && <section className="panel"><div className="panel-head"><h3>Consumers ({active.length})</h3><button className="primary small" onClick={()=>setShowAdd(true)}>+ Add</button></div>{consumers.map(c=><div className="row consumer" key={c.id}><div className="avatar">{c.name?.[0]?.toUpperCase()}</div><div className="grow"><b>{c.name}</b><small>{c.consumer_id} · {c.phone} · {c.meal_plan}</small></div><span className={c.active?'badge ok':'badge'}>{c.active?'Active':'Inactive'}</span><button className="ghost" onClick={()=>showQR(c)}>Show QR</button></div>)}{!consumers.length&&<Empty text="Add your first consumer."/>}</section>}
  {tab==='attendance' && <div className="grid"><section className="panel scanner-panel"><h3>Scan meal QR</h3><div className="meal-tabs">{['breakfast','lunch','dinner'].map(m=><button className={meal===m?'selected':''} onClick={()=>setMeal(m)} key={m}>{m}</button>)}</div><div id="messmate-reader"></div>{!scanner&&<button className="primary full" onClick={startScanner}>Open camera scanner</button>}{scanner&&<button className="ghost full" onClick={stopScanner}>Stop camera</button>}<div className="divider">or enter QR token</div><input value={scan} onChange={e=>setScan(e.target.value)} placeholder="QR token UUID"/><button className="primary full" disabled={!scan} onClick={()=>markAttendance(scan)}>Mark attendance</button></section><section className="panel"><h3>Today's attendance</h3>{attendance.map((a,i)=><div className="row" key={a.id||i}><div className="grow"><b>{a.name||a.consumer_name||a.consumer_id}</b><small>{a.meal}</small></div><span className="badge ok">Present</span></div>)}{!attendance.length&&<Empty text="Nothing scanned yet."/>}</section></div>}
  {tab==='payments' && <section className="panel"><div className="panel-head"><h3>Pending payments</h3></div>{pending.map((p,i)=><div className="row" key={p.id||i}><div className="grow"><b>{p.name||p.consumer_name||p.consumer_id}</b><small>{p.month_label||'Subscription'} · Due {p.amount!=null?`₹${p.amount}`:''}</small></div><span className="badge pending">Pending</span></div>)}{!pending.length&&<Empty text="No pending payments."/>}</section>}
  </section>
  {showAdd&&<Modal title="Add consumer" close={()=>setShowAdd(false)}><form onSubmit={addConsumer}><label>Name<input required value={form.name} onChange={e=>setForm({...form,name:e.target.value})}/></label><label>Phone<input required value={form.phone} onChange={e=>setForm({...form,phone:e.target.value})}/></label><label>Consumer ID<input value={form.consumer_id} onChange={e=>setForm({...form,consumer_id:e.target.value})} placeholder="Auto-generated if empty"/></label><label>Meal plan<select value={form.meal_plan} onChange={e=>setForm({...form,meal_plan:e.target.value})}><option value="all">All meals</option><option value="breakfast">Breakfast</option><option value="lunch">Lunch</option><option value="dinner">Dinner</option></select></label><button className="primary full">Add consumer</button></form></Modal>}
  {qr&&<Modal title={`${qr.name} — QR code`} close={()=>setQr(null)}><div className="qr-card"><img src={`data:image/png;base64,${qr.image_base64}`} alt="Consumer QR"/><b>{qr.consumer_id}</b><small>Scan this QR for meal attendance.</small></div></Modal>}
  </div>
}
function Empty({text}){return <div className="empty">{text}</div>}
function Modal({title,close,children}){return <div className="modal-backdrop" onClick={close}><div className="modal" onClick={e=>e.stopPropagation()}><div className="panel-head"><h3>{title}</h3><button className="close" onClick={close}>×</button></div>{children}</div></div>}
function App(){const [logged,setLogged]=useState(!!localStorage.getItem('messmate_token'));return logged?<Dashboard onLogout={()=>setLogged(false)}/>:<Auth onLogin={()=>setLogged(true)}/>}

createRoot(document.getElementById('root')).render(<App/>);
