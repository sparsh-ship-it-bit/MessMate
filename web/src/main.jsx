import React,{useEffect,useMemo,useState} from 'react';
import {createRoot} from 'react-dom/client';
import {Html5Qrcode} from 'html5-qrcode';
import './styles.css';
const API=import.meta.env.VITE_API_URL||'';const meals=['breakfast','lunch','dinner'];
const today=()=>{const parts=new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Kolkata',year:'numeric',month:'2-digit',day:'2-digit'}).formatToParts(new Date());const part=type=>parts.find(p=>p.type===type)?.value||'';return `${part('year')}-${part('month')}-${part('day')}`};const currentMonth=()=>today().slice(0,7);
const label=p=>p==='all'?'All meals':(p||'').split(',').filter(Boolean).map(x=>x[0].toUpperCase()+x.slice(1)).join(' + ');
const money=n=>`₹${Number(n||0).toLocaleString('en-IN',{maximumFractionDigits:2})}`;
const statusText=s=>({paid:'Paid',partial:'Partially Paid',pending:'Pending',overdue:'Overdue'}[s]||s||'Pending');
const statusClass=s=>`badge ${s==='paid'?'ok':s==='overdue'?'overdue':s==='partial'?'partial':'pending'}`;
async function api(path,opts={}){const token=localStorage.getItem('messmate_token');const headers={'Content-Type':'application/json',...(opts.headers||{})};if(token)headers.Authorization=`Bearer ${token}`;const r=await fetch(API+path,{...opts,headers});const t=await r.text();let d={};try{d=t?JSON.parse(t):{}}catch{d={error:t}}if(!r.ok)throw Error(d.error||`Request failed (${r.status})`);return d}
function WebUpdateWatcher({defer=false}){
 const [update,setUpdate]=useState(false),[snoozedUntil,setSnoozedUntil]=useState(0);
 useEffect(()=>{let stopped=false;const check=async()=>{try{const currentPath=new URL(import.meta.url).pathname;const response=await fetch('/index.html?messmate-update-check='+Date.now(),{cache:'no-store',headers:{'Cache-Control':'no-cache'}});if(!response.ok)return;const html=await response.text();const match=html.match(/<script[^>]+type=[\"']module[\"'][^>]+src=[\"']([^\"']+)[\"']/i)||html.match(/<script[^>]+src=[\"']([^\"']+)[\"'][^>]+type=[\"']module[\"']/i);if(!match)return;const nextPath=new URL(match[1],window.location.origin).pathname;if(!stopped&&nextPath!==currentPath)setUpdate(true)}catch{}};const id=setInterval(check,30000);const initial=setTimeout(check,15000);return()=>{stopped=true;clearInterval(id);clearTimeout(initial)}},[]);
 const visible=update&&!defer&&Date.now()>=snoozedUntil;
 if(!visible)return null;
 return <div role="dialog" aria-live="polite" style={{position:'fixed',right:20,bottom:20,zIndex:100000,width:'min(420px, calc(100vw - 32px))',padding:20,borderRadius:18,background:'#101a13',border:'1px solid rgba(183,255,98,.55)',boxShadow:'0 18px 60px rgba(0,0,0,.5)',color:'#fff'}}><b style={{fontSize:17}}>MessMate update available</b><p style={{color:'#b8c5b9',fontSize:13,lineHeight:1.5,margin:'8px 0 16px'}}>A newer version is ready. Refresh now to get the latest improvements, or choose Later to keep working.</p><div style={{display:'flex',justifyContent:'flex-end',gap:8}}><button type="button" className="ghost" onClick={()=>setSnoozedUntil(Date.now()+5*60*1000)}>Later</button><button type="button" className="primary" onClick={()=>window.location.reload()}>Refresh now</button></div></div>;
}
function MessMateLogo(){return <svg className="mm-logo-art" viewBox="0 0 100 100" role="img" aria-label="MessMate black M on neon-lime background"><path d="M18 76V28L50 57L82 28V76" fill="none" stroke="#050805" strokeWidth="13" strokeLinecap="round" strokeLinejoin="round"/></svg>}
