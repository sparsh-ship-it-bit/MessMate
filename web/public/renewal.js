(function(){
  const money=n=>`₹${Number(n||0).toLocaleString('en-IN',{maximumFractionDigits:2})}`;
  const token=()=>localStorage.getItem('messmate_token')||'';
  async function getConsumers(){
    const r=await fetch('/api/v1/consumers',{headers:{Authorization:'Bearer '+token()}});
    const d=await r.json(); if(!r.ok) throw Error(d.error||'Could not load consumer'); return d;
  }
  async function getRenewInfo(id){
    const r=await fetch('/api/v1/subscriptions/renew-info/'+encodeURIComponent(id),{headers:{Authorization:'Bearer '+token()}});
    const d=await r.json(); if(!r.ok) throw Error(d.error||'Could not load renewal details'); return d;
  }
  function addStyles(){
    if(document.getElementById('mm-renew-style')) return;
    const s=document.createElement('style');s.id='mm-renew-style';s.textContent=`
      .mm-renew-backdrop{position:fixed;inset:0;background:rgba(0,0,0,.72);backdrop-filter:blur(8px);display:grid;place-items:center;z-index:99999;padding:18px}
      .mm-renew-modal{width:min(520px,100%);background:#0c1711;border:1px solid rgba(184,255,101,.22);border-radius:24px;padding:24px;box-shadow:0 30px 90px rgba(0,0,0,.55);color:#fff;font-family:inherit}
      .mm-renew-head{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.mm-renew-head h3{margin:0;font-size:22px}.mm-renew-close{border:0;background:transparent;color:#aaa;font-size:28px;cursor:pointer}
      .mm-renew-sub{color:#9eaba3;font-size:13px;margin:5px 0 22px}.mm-renew-options{display:grid;grid-template-columns:1fr 1fr;gap:12px}
      .mm-renew-option{border:1px solid rgba(184,255,101,.18);background:#101f15;color:#fff;border-radius:18px;padding:18px;text-align:left;cursor:pointer}.mm-renew-option:hover{border-color:#b8ff65;background:#14291a}.mm-renew-option strong{display:block;font-size:18px}.mm-renew-option span{display:block;color:#aab5ad;font-size:12px;margin-top:5px}.mm-renew-price{font-size:24px!important;color:#b8ff65!important;margin-top:13px!important;font-weight:800}
      .mm-renew-field{display:block;margin-top:4px}.mm-renew-field span{display:block;color:#aeb9b1;font-size:12px;margin-bottom:8px}.mm-renew-select{width:100%;box-sizing:border-box;background:#101f15;color:#fff;border:1px solid rgba(184,255,101,.22);border-radius:12px;padding:12px 14px;font:inherit;outline:none}.mm-renew-select:focus{border-color:#b8ff65}
      .mm-renew-summary{margin-top:14px;padding:14px;border-radius:14px;background:rgba(184,255,101,.06);border:1px solid rgba(184,255,101,.1)}.mm-renew-summary strong{display:block;color:#b8ff65;font-size:18px}.mm-renew-summary span{display:block;color:#aeb9b1;font-size:12px;margin-top:4px}
      .mm-renew-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:18px}.mm-renew-btn{border:0;border-radius:12px;padding:11px 16px;font-weight:700;cursor:pointer}.mm-renew-cancel{background:#17231b;color:#cbd3ce}.mm-renew-save{background:#b8ff65;color:#071008}.mm-renew-save:disabled{opacity:.55;cursor:not-allowed}
      .mm-renew-error{color:#ff8585;margin-top:12px;font-size:13px}
      @media(max-width:520px){.mm-renew-options{grid-template-columns:1fr}.mm-renew-modal{padding:20px}}
    `;document.head.appendChild(s);
  }
  function monthOptions(){
    const now=new Date();
    const out=[];
    for(let i=0;i<24;i++){
      const x=new Date(now.getFullYear(),now.getMonth()-i,1);
      const value=`${x.getFullYear()}-${String(x.getMonth()+1).padStart(2,'0')}`;
      const label=x.toLocaleString('en-IN',{month:'long',year:'numeric'});
      out.push(`<option value="${value}">${label}</option>`);
    }
    return out.join('');
  }
  function selectedDates(month,period){
    const parts=month.split('-').map(Number);const y=parts[0],m=parts[1];
    const start=new Date(y,m-1,1);const end=period==='half'?new Date(y,m-1,15):new Date(y,m,0);
    const fmt=d=>`${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;
    return {start:fmt(start),end:fmt(end)};
  }
  function showRenewal(c,monthly){
    addStyles();
    const old=document.getElementById('mm-renew-backdrop');if(old)old.remove();
    const name=c.name||'Consumer';
    const el=document.createElement('div');el.id='mm-renew-backdrop';el.className='mm-renew-backdrop';
    el.innerHTML=`<div class="mm-renew-modal"><div class="mm-renew-head"><div><h3>Renew ${name}</h3><div class="mm-renew-sub">First choose the duration, then choose the month to record.</div></div><button class="mm-renew-close" type="button">×</button></div><div class="mm-renew-step" data-step="duration"><div class="mm-renew-options"><button class="mm-renew-option" data-period="half" type="button"><strong>Half Month</strong><span>1st–15th of selected month</span><span class="mm-renew-price">${money(monthly/2)}</span></button><button class="mm-renew-option" data-period="full" type="button"><strong>Full Month</strong><span>Entire selected calendar month</span><span class="mm-renew-price">${money(monthly)}</span></button></div></div><div class="mm-renew-step" data-step="month" hidden><label class="mm-renew-field"><span>Choose month</span><select class="mm-renew-select">${monthOptions()}</select></label><div class="mm-renew-summary"><strong class="mm-renew-amount"></strong><span class="mm-renew-dates"></span></div><div class="mm-renew-actions"><button class="mm-renew-btn mm-renew-cancel" type="button">Back</button><button class="mm-renew-btn mm-renew-save" type="button">Save renewal</button></div></div><div class="mm-renew-error" hidden></div></div>`;
    document.body.appendChild(el);
    const close=()=>el.remove();
    el.querySelector('.mm-renew-close').onclick=close;
    el.onclick=e=>{if(e.target===el)close()};
    let period='';
    const durationStep=el.querySelector('[data-step="duration"]');
    const monthStep=el.querySelector('[data-step="month"]');
    const select=el.querySelector('.mm-renew-select');
    const amount=el.querySelector('.mm-renew-amount');
    const dates=el.querySelector('.mm-renew-dates');
    const error=el.querySelector('.mm-renew-error');
    const refresh=()=>{const d=selectedDates(select.value,period);amount.textContent=period==='half'?`Half month · ${money(monthly/2)}`:`Full month · ${money(monthly)}`;dates.textContent=`Coverage: ${d.start} to ${d.end}`};
    el.querySelectorAll('[data-period]').forEach(b=>b.onclick=()=>{period=b.dataset.period;durationStep.hidden=true;monthStep.hidden=false;refresh()});
    select.onchange=refresh;
    el.querySelector('.mm-renew-cancel').onclick=()=>{monthStep.hidden=true;durationStep.hidden=false;error.hidden=true};
    el.querySelector('.mm-renew-save').onclick=async()=>{
      error.hidden=true;const save=el.querySelector('.mm-renew-save');save.disabled=true;
      try{
        const r=await fetch('/api/v1/subscriptions/renew',{method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+token()},body:JSON.stringify({consumer_id:c.id,period,month:select.value})});
        const d=await r.json();if(!r.ok)throw Error(d.error||'Renewal failed');
        close();window.location.reload();
      }catch(e){error.textContent=e.message;error.hidden=false;save.disabled=false}
    };
  }
  document.addEventListener('click',async e=>{
    const b=e.target.closest&&e.target.closest('button');if(!b||b.textContent.trim()!=='Renew')return;
    e.preventDefault();e.stopImmediatePropagation();
    try{
      const row=b.closest('.row');const small=row&&row.querySelector('.grow small');const cid=small?.textContent.split('·')[0].trim();
      if(!cid)throw Error('Could not identify consumer');
      const consumers=await getConsumers();const c=consumers.find(x=>x.consumer_id===cid);if(!c)throw Error('Consumer not found');
      const info=await getRenewInfo(c.id);showRenewal(c,Number(info.monthly_amount||0));
    }catch(e){alert(e.message)}
  },true);
})();
