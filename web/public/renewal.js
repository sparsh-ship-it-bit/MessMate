(function(){
  const money=n=>`₹${Number(n||0).toLocaleString('en-IN',{maximumFractionDigits:2})}`;
  const token=()=>localStorage.getItem('messmate_token')||'';
  const api=async(path,opts={})=>{
    const r=await fetch(path,{...opts,headers:{'Content-Type':'application/json',Authorization:'Bearer '+token(),...(opts.headers||{})}});
    const t=await r.text();let d={};try{d=t?JSON.parse(t):{}}catch{d={error:t}}
    if(!r.ok)throw Error(d.error||'Request failed');return d;
  };
  const monthLabel=v=>{const [y,m]=v.split('-');return new Date(Number(y),Number(m)-1,1).toLocaleString('en-IN',{month:'long',year:'numeric'})};
  const months=()=>{
    const d=new Date(),out=[];
    for(let i=0;i<24;i++){const x=new Date(d.getFullYear(),d.getMonth()-i,1),v=x.toISOString().slice(0,7);out.push({v,l:monthLabel(v)})}
    return out;
  };
  function styles(){
    if(document.getElementById('mm-pay-style'))return;
    const s=document.createElement('style');s.id='mm-pay-style';s.textContent=`
      .mm-pay-backdrop{position:fixed;inset:0;background:rgba(0,0,0,.78);backdrop-filter:blur(9px);display:grid;place-items:center;z-index:99999;padding:16px}
      .mm-pay-modal{width:min(620px,100%);max-height:92vh;overflow:auto;background:#0d1810;border:1px solid rgba(183,255,98,.24);border-radius:24px;padding:22px;color:#fff;box-shadow:0 30px 90px rgba(0,0,0,.6)}
      .mm-pay-head{display:flex;justify-content:space-between;align-items:flex-start;gap:12px}.mm-pay-head h3{margin:0;font-size:22px}.mm-pay-close{border:1px solid #29372c;background:#182219;border-radius:9px;color:#b7c1b7;font-size:22px;width:34px;height:34px}
      .mm-pay-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.mm-pay-modal label{display:block;color:#aab5ad;font-size:11px;font-weight:700;margin:14px 0}.mm-pay-modal select,.mm-pay-modal input{width:100%;margin-top:6px;border:1px solid #2a392d;border-radius:11px;padding:11px;background:#0b150d;color:#fff}
      .mm-pay-periods{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin:12px 0}.mm-pay-period{border:1px solid #2a392d;background:#101d13;border-radius:14px;padding:14px;text-align:left;color:#fff}.mm-pay-period.selected{border-color:#b7ff62;background:#17301c}.mm-pay-period b{display:block}.mm-pay-period small{display:block;color:#7f8b81;margin-top:4px}.mm-pay-amount{font-size:22px;font-weight:900;color:#b7ff62;margin-top:10px}
      .mm-pay-qr{display:grid;place-items:center;margin:16px 0;padding:16px;border:1px solid #26362a;background:#0a120c;border-radius:18px}.mm-pay-qr img{width:260px;height:260px;background:#fff;border-radius:12px;padding:8px}.mm-pay-qr b{margin-top:10px}.mm-pay-qr small{color:#7f8b81;margin-top:3px}
      .mm-pay-note{font-size:11px;line-height:1.5;color:#89958b;background:#121f15;border-radius:11px;padding:11px;margin:12px 0}.mm-pay-error{color:#ff8585;font-size:12px;margin:10px 0}.mm-pay-success{color:#b7ff62;font-size:12px;margin:10px 0}
      .mm-pay-actions{display:flex;gap:9px;flex-wrap:wrap}.mm-pay-actions button{border:0;border-radius:10px;padding:11px 14px;font-weight:800}.mm-pay-primary{background:#b7ff62;color:#10200d}.mm-pay-ghost{background:#111b13;border:1px solid #2c392e!important;color:#fff}.mm-pay-config{display:flex;gap:8px}.mm-pay-config input{flex:1;margin:0}
      @media(max-width:560px){.mm-pay-grid,.mm-pay-periods{grid-template-columns:1fr}.mm-pay-modal{padding:18px}}
    `;document.head.appendChild(s);
  }
  function closeModal(){document.getElementById('mm-pay-backdrop')?.remove()}
  async function openPayment(){
    styles();closeModal();
    let consumers;
    try{consumers=await api('/api/v1/consumers')}catch(e){alert(e.message);return}
    let provider;
    try{provider=await api('/api/v1/payments/provider-status')}catch(e){alert(e.message);return}
    if(!provider.configured){alert('Online payments are not configured yet. Add Razorpay server credentials and webhook secret in Render.');return}
    if(!provider.route_enabled){
      const accountId=window.prompt('MessMate needs this owner\'s Razorpay Route Linked Account ID (starts with acc_). Create/onboard the linked account in Razorpay Route first, then paste the account ID here:','');
      if(!accountId||!accountId.trim()){alert('Razorpay Route linked account is required for automatic settlement to this mess owner.');return}
      try{await api('/api/v1/payments/razorpay-account',{method:'PUT',body:JSON.stringify({razorpay_account_id:accountId.trim()})})}
      catch(e){alert(e.message);return}
    }
    const el=document.createElement('div');el.id='mm-pay-backdrop';el.className='mm-pay-backdrop';
    const firstMonth=months()[0].v;
    el.innerHTML=\`<div class="mm-pay-modal">
      <div class="mm-pay-head"><div><h3>Collect payment</h3><div style="color:#7f8b81;font-size:12px;margin-top:4px">Payment is verified automatically. No UTR or owner confirmation is required.</div></div><button class="mm-pay-close" type="button">×</button></div>
      <div class="mm-pay-grid">
        <label>Consumer<select id="mm-consumer">\${consumers.map(c=>\`<option value="\${c.id}">\${c.name} — \${c.consumer_id}</option>\`).join('')}</select></label>
        <label>Month<select id="mm-month">\${months().map(m=>\`<option value="\${m.v}" \${m.v===firstMonth?'selected':''}>\${m.l}</option>\`).join('')}</select></label>
      </div>
      <div class="mm-pay-periods">
        <button type="button" class="mm-pay-period selected" data-period="full"><b>Full Month</b><small>1st to last day</small><div class="mm-pay-amount" id="mm-full">—</div></button>
        <button type="button" class="mm-pay-period" data-period="half"><b>Half Month</b><small>1st to 15th</small><div class="mm-pay-amount" id="mm-half">—</div></button>
      </div>
      <div id="mm-qr-area"></div>
      <div class="mm-pay-actions"><button type="button" class="mm-pay-primary" id="mm-generate">Generate secure QR</button></div>
      <div id="mm-pay-message"></div>
    </div>\`;
    document.body.appendChild(el);
    el.querySelector('.mm-pay-close').onclick=closeModal;
    el.onclick=e=>{if(e.target===el)closeModal()};
    let period='full',monthly=0;
    const consumer=()=>consumers.find(x=>x.id===el.querySelector('#mm-consumer').value);
    const msg=t=>{el.querySelector('#mm-pay-message').innerHTML=t?'<div class="mm-pay-error">'+t+'</div>':''};
    const loadInfo=async()=>{
      msg('');
      try{const d=await api('/api/v1/subscriptions/renew-info/'+encodeURIComponent(consumer().id));monthly=Number(d.monthly_amount||0);el.querySelector('#mm-full').textContent=money(monthly);el.querySelector('#mm-half').textContent=money(monthly/2)}catch(e){msg(e.message)}
    };
    el.querySelectorAll('.mm-pay-period').forEach(b=>b.onclick=()=>{period=b.dataset.period;el.querySelectorAll('.mm-pay-period').forEach(x=>x.classList.toggle('selected',x===b))});
    el.querySelector('#mm-consumer').onchange=loadInfo;
    async function generateQR(){
      msg('');
      const amount=period==='half'?monthly/2:monthly;
      if(!amount){msg('Monthly amount is not configured for this consumer.');return}
      const b=el.querySelector('#mm-generate');b.disabled=true;b.textContent='Generating…';
      try{
        const d=await api('/api/v1/payments/qr',{method:'POST',body:JSON.stringify({consumer_id:consumer().id,month:el.querySelector('#mm-month').value,period,amount})});
        el.querySelector('#mm-qr-area').innerHTML=\`<div class="mm-pay-qr">
          <img id="mm-qr" src="\${d.image_url}" alt="Secure payment QR">
          <b>\${money(amount)} · \${monthLabel(el.querySelector('#mm-month').value)}</b>
          <small>Scan this QR to pay securely</small>
          <small style="color:#b7ff62">Waiting for payment confirmation…</small>
        </div>\`;
        b.textContent='QR generated';
        pollStatus(d.intent_id);
      }catch(e){msg(e.message);b.disabled=false;b.textContent='Generate secure QR'}
    }
    let pollTimer;
    async function pollStatus(intentId){
      clearTimeout(pollTimer);
      try{
        const d=await api('/api/v1/payments/qr/'+encodeURIComponent(intentId)+'/status');
        if(d.status==='paid'){
          el.querySelector('#mm-qr-area').innerHTML='<div class="mm-pay-qr"><div style="font-size:44px">✓</div><b style="color:#b7ff62">Payment received & subscription renewed</b><small>MessMate verified the payment automatically.</small></div>';
          setTimeout(()=>window.location.reload(),1200);return;
        }
        if(d.status==='failed'){msg('This payment request could not be completed. Generate a new QR.');return}
      }catch(e){}
      pollTimer=setTimeout(()=>pollStatus(intentId),2500);
    }
    el.querySelector('#mm-generate').onclick=generateQR;
    await loadInfo();
  }
  function showRenewal(c){
    styles();closeModal();
    const el=document.createElement('div');el.id='mm-pay-backdrop';el.className='mm-pay-backdrop';
    el.innerHTML=`<div class="mm-pay-modal"><div class="mm-pay-head"><div><h3>Renew ${c.name||'Consumer'}</h3><div style="color:#7f8b81;font-size:12px">Choose duration and historical month.</div></div><button class="mm-pay-close">×</button></div><div class="mm-pay-periods"><button class="mm-pay-period selected" data-period="full"><b>Full Month</b><small>1st to last day</small><div class="mm-pay-amount">—</div></button><button class="mm-pay-period" data-period="half"><b>Half Month</b><small>1st to 15th</small><div class="mm-pay-amount">—</div></button></div><label>Month<select id="mm-month">${months().map(m=>`<option value="${m.v}">${m.l}</option>`).join('')}</select></label><div id="mm-renew-message"></div><button class="mm-pay-primary" id="mm-renew-save" style="width:100%;margin-top:8px">Renew selected month</button></div>`;
    document.body.appendChild(el);el.querySelector('.mm-pay-close').onclick=closeModal;el.onclick=e=>{if(e.target===el)closeModal()};
    let period='full',monthly=0;
    const msg=t=>el.querySelector('#mm-renew-message').innerHTML=t?'<div class="mm-pay-error">'+t+'</div>':'';
    const infoLoad=async()=>{try{const d=await api('/api/v1/subscriptions/renew-info/'+encodeURIComponent(c.id));monthly=Number(d.monthly_amount||0);el.querySelectorAll('.mm-pay-period .mm-pay-amount')[0].textContent=money(monthly);el.querySelectorAll('.mm-pay-period .mm-pay-amount')[1].textContent=money(monthly/2);if(d.end_date){const end=new Date(d.end_date+'T00:00:00');const next=new Date(end.getFullYear(),end.getMonth()+1,1);const nextMonth=next.toISOString().slice(0,7);const select=el.querySelector('#mm-month');if([...select.options].some(o=>o.value===nextMonth))select.value=nextMonth}}catch(e){msg(e.message)}};
    el.querySelectorAll('.mm-pay-period').forEach(b=>b.onclick=()=>{period=b.dataset.period;el.querySelectorAll('.mm-pay-period').forEach(x=>x.classList.toggle('selected',x===b))});
    el.querySelector('#mm-renew-save').onclick=async()=>{const b=el.querySelector('#mm-renew-save');b.disabled=true;b.textContent='Saving…';try{await api('/api/v1/subscriptions/renew',{method:'POST',body:JSON.stringify({consumer_id:c.id,period,month:el.querySelector('#mm-month').value,method:'cash'})});closeModal();window.location.reload()}catch(e){msg(e.message);b.disabled=false;b.textContent='Renew selected month'}};
    infoLoad();
  }
  async function interceptRenew(e){
    const b=e.target.closest&&e.target.closest('button');if(!b||b.textContent.trim()!=='Renew')return;
    e.preventDefault();e.stopImmediatePropagation();
    try{const row=b.closest('.row');const small=row&&row.querySelector('.grow small');const cid=small?.textContent.split('·')[0].trim();if(!cid)throw Error('Could not identify consumer');const cs=await api('/api/v1/consumers');const c=cs.find(x=>x.consumer_id===cid);if(!c)throw Error('Consumer not found');showRenewal(c)}catch(err){alert(err.message)}
  }
  document.addEventListener('click',interceptRenew,true);
  const inject=()=>{
    const h=[...document.querySelectorAll('h3')].find(x=>x.textContent.trim().startsWith('Outstanding payments'));
    if(!h||document.getElementById('mm-collect-btn'))return;
    const btn=document.createElement('button');btn.id='mm-collect-btn';btn.className='primary small';btn.textContent='Collect via QR';btn.onclick=openPayment;
    h.parentElement.appendChild(btn);
  };
  new MutationObserver(inject).observe(document.body,{childList:true,subtree:true});
  setInterval(inject,1200);
})();