(function(){
  const money=n=>`₹${Number(n||0).toLocaleString('en-IN',{maximumFractionDigits:2})}`;
  const token=()=>localStorage.getItem('messmate_token')||'';
  async function getConsumers(){
    const r=await fetch('/api/v1/consumers',{headers:{Authorization:'Bearer '+token()}});
    const d=await r.json(); if(!r.ok) throw Error(d.error||'Could not load consumer'); return d;
  }
  function addStyles(){
    if(document.getElementById('mm-renew-style')) return;
    const s=document.createElement('style');s.id='mm-renew-style';s.textContent=`
      .mm-renew-backdrop{position:fixed;inset:0;background:rgba(0,0,0,.72);backdrop-filter:blur(8px);display:grid;place-items:center;z-index:99999;padding:18px}
      .mm-renew-modal{width:min(520px,100%);background:#0c1711;border:1px solid rgba(184,255,101,.22);border-radius:24px;padding:24px;box-shadow:0 30px 90px rgba(0,0,0,.55);color:#fff;font-family:inherit}
      .mm-renew-head{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.mm-renew-head h3{margin:0;font-size:22px}.mm-renew-close{border:0;background:transparent;color:#aaa;font-size:28px;cursor:pointer}
      .mm-renew-sub{color:#9eaba3;font-size:13px;margin:5px 0 22px}.mm-renew-options{display:grid;grid-template-columns:1fr 1fr;gap:12px}
      .mm-renew-option{border:1px solid rgba(184,255,101,.18);background:#101f15;color:#fff;border-radius:18px;padding:18px;text-align:left;cursor:pointer}.mm-renew-option:hover{border-color:#b8ff65;background:#14291a}.mm-renew-option strong{display:block;font-size:18px}.mm-renew-option span{display:block;color:#aab5ad;font-size:12px;margin-top:5px}.mm-renew-price{font-size:24px!important;color:#b8ff65!important;margin-top:13px!important;font-weight:800}
      .mm-renew-note{margin-top:16px;padding:12px 14px;border-radius:12px;background:rgba(184,255,101,.06);color:#aeb9b1;font-size:12px;line-height:1.5}.mm-renew-error{color:#ff8585;margin-top:12px;font-size:13px}
      @media(max-width:520px){.mm-renew-options{grid-template-columns:1fr}.mm-renew-modal{padding:20px}}
    `;document.head.appendChild(s);
  }
  function showRenewal(c){
    addStyles();
    const old=document.getElementById('mm-renew-backdrop');if(old)old.remove();
    const monthly=Number(c.subscription?.monthly_amount||c.subscription?.amount||0);
    const name=c.name||'Consumer';
    const el=document.createElement('div');el.id='mm-renew-backdrop';el.className='mm-renew-backdrop';
    el.innerHTML=`<div class="mm-renew-modal"><div class="mm-renew-head"><div><h3>Renew ${name}</h3><div class="mm-renew-sub">Choose the coverage you are collecting payment for.</div></div><button class="mm-renew-close" type="button">×</button></div><div class="mm-renew-options"><button class="mm-renew-option" data-period="half" type="button"><strong>Half Month</strong><span>15 days of access</span><span class="mm-renew-price">${money(monthly/2)}</span></button><button class="mm-renew-option" data-period="full" type="button"><strong>Full Month</strong><span>1 month of access</span><span class="mm-renew-price">${money(monthly)}</span></button></div><div class="mm-renew-note">Payment is recorded immediately. The new subscription starts after the current expiry date, and attendance access follows the renewed period.</div><div class="mm-renew-error" hidden></div></div>`;
    document.body.appendChild(el);
    const close=()=>el.remove();el.querySelector('.mm-renew-close').onclick=close;el.onclick=e=>{if(e.target===el)close()};
    el.querySelectorAll('[data-period]').forEach(b=>b.onclick=async()=>{
      const err=el.querySelector('.mm-renew-error');err.hidden=true;b.disabled=true;
      try{
        const r=await fetch('/api/v1/subscriptions/renew',{method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+token()},body:JSON.stringify({consumer_id:c.id,period:b.dataset.period})});
        const d=await r.json();if(!r.ok)throw Error(d.error||'Renewal failed');
        close();window.location.reload();
      }catch(e){err.textContent=e.message;err.hidden=false;b.disabled=false}
    });
  }
  document.addEventListener('click',async e=>{
    const b=e.target.closest&&e.target.closest('button');if(!b||b.textContent.trim()!=='Renew')return;
    e.preventDefault();e.stopImmediatePropagation();
    try{
      const row=b.closest('.row');const small=row&&row.querySelector('.grow small');const cid=small?.textContent.split('·')[0].trim();
      if(!cid)throw Error('Could not identify consumer');
      const consumers=await getConsumers();const c=consumers.find(x=>x.consumer_id===cid);if(!c)throw Error('Consumer not found');showRenewal(c);
    }catch(e){alert(e.message)}
  },true);
})();
