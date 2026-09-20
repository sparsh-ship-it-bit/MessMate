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
    let consumers,settings;
    try{[consumers,settings]=await Promise.all([api('/api/v1/consumers'),api('/api/v1/payments/upi-settings')])}catch(e){alert(e.message);return}
    const el=document.createElement('div');el.id='mm-pay-backdrop';el.className='mm-pay-backdrop';
    const firstMonth=months()[0].v;
    el.innerHTML=`<div class="mm-pay-modal">
      <div class="mm-pay-head"><div><h3>Collect payment</h3><div style="color:#7f8b81;font-size:12px;margin-top:4px">Select consumer → month → plan → show the mess owner's payment QR.</div></div><button class="mm-pay-close" type="button">×</button></div>
      <div class="mm-pay-grid">
        <label>Consumer<select id="mm-consumer">${consumers.map(c=>`<option value="${c.id}">${c.name} — ${c.consumer_id}</option>`).join('')}</select></label>
        <label>Month<select id="mm-month">${months().map(m=>`<option value="${m.v}" ${m.v===firstMonth?'selected':''}>${m.l}</option>`).join('')}</select></label>
      </div>
      <div class="mm-pay-periods">
        <button type="button" class="mm-pay-period selected" data-period="full"><b>Full Month</b><small>1st to last day</small><div class="mm-pay-amount" id="mm-full">—</div></button>
        <button type="button" class="mm-pay-period" data-period="half"><b>Half Month</b><small>1st to 15th</small><div class="mm-pay-amount" id="mm-half">—</div></button>
      </div>
      <div id="mm-qr-area"></div>
      <div class="mm-pay-actions"><button type="button" class="mm-pay-primary" id="mm-generate">Show payment QR</button><button type="button" class="mm-pay-ghost" id="mm-config">Set owner UPI ID</button></div>
      <div id="mm-pay-message"></div>
    </div>`;
    document.body.appendChild(el);
    el.querySelector('.mm-pay-close').onclick=closeModal;
    el.onclick=e=>{if(e.target===el)closeModal()};
    let period='full',monthly=0,info=null;
    const consumer=()=>consumers.find(c=>c.id===el.querySelector('#mm-consumer').value);
    const msg=t=>{el.querySelector('#mm-pay-message').innerHTML=t?'<div class="mm-pay-error">'+t+'</div>':''};
    const loadInfo=async()=>{
      msg('');
      try{info=await api('/api/v1/subscriptions/renew-info/'+encodeURIComponent(consumer().id));monthly=Number(info.monthly_amount||0);el.querySelector('#mm-full').textContent=money(monthly);el.querySelector('#mm-half').textContent=money(monthly/2)}catch(e){msg(e.message)}
    };
    const setPeriod=p=>{period=p;el.querySelectorAll('.mm-pay-period').forEach(b=>b.classList.toggle('selected',b.dataset.period===p));if(el.querySelector('#mm-qr'))generateQR()};
    el.querySelectorAll('.mm-pay-period').forEach(b=>b.onclick=()=>setPeriod(b.dataset.period));
    el.querySelector('#mm-consumer').onchange=loadInfo;
    el.querySelector('#mm-month').onchange=()=>{if(el.querySelector('#mm-qr'))generateQR()};
    async function generateQR(){
      msg('');const amount=period==='half'?monthly/2:monthly;if(!amount){msg('Monthly amount is not configured for this consumer.');return}
      try{
        const d=await api('/api/v1/payments/upi-qr',{method:'POST',body:JSON.stringify({consumer_id:consumer().id,month:el.querySelector('#mm-month').value,period,amount})});
        el.querySelector('#mm-qr-area').innerHTML=`<div class="mm-pay-qr"><img id="mm-qr" src="data:image/png;base64,${d.image_base64}" alt="Payment QR"><b>${money(amount)} · ${monthLabel(el.querySelector('#mm-month').value)}</b><small>Pay to ${d.upi_id}</small><small>After payment, enter the UTR below to confirm and renew.</small><div style="width:100%;margin-top:12px"><input id="mm-utr" placeholder="UPI UTR / transaction ID"></div><button type="button" class="mm-pay-primary" id="mm-confirm" style="margin-top:10px;width:100%">Confirm payment & renew</button></div>`;
        el.querySelector('#mm-confirm').onclick=confirmPayment;
      }catch(e){msg(e.message)}
    }
    async function confirmPayment(){
      const ref=(el.querySelector('#mm-utr')?.value||'').trim();if(!ref){msg('Enter the UTR / transaction ID after verifying the payment.');return}
      const b=el.querySelector('#mm-confirm');b.disabled=true;b.textContent='Renewing…';msg('');
      try{
        await api('/api/v1/subscriptions/renew',{method:'POST',body:JSON.stringify({consumer_id:consumer().id,period,month:el.querySelector('#mm-month').value,method:'upi',reference:ref})});
        el.querySelector('#mm-pay-message').innerHTML='<div class="mm-pay-success">Payment recorded and subscription renewed successfully.</div>';
        setTimeout(()=>window.location.reload(),700);
      }catch(e){msg(e.message);b.disabled=false;b.textContent='Confirm payment & renew'}
    }
    el.querySelector('#mm-generate').onclick=generateQR;
    el.querySelector('#mm-config').onclick=async()=>{
      const current=settings.upi_id||'';const upi=prompt('Enter the mess owner UPI ID (example: owner@upi):',current);
      if(upi===null)return;
      try{settings=await api('/api/v1/payments/upi-settings',{method:'PUT',body:JSON.stringify({upi_id:upi.trim()})});await generateQR()}catch(e){msg(e.message)}
    };
    if(!settings.upi_id)msg('Set the mess owner UPI ID before generating a payment QR.');
    await loadInfo();
  }
  function showRenewal(c){
    styles();closeModal();
    const el=document.createElement('div');el.id='mm-pay-backdrop';el.className='mm-pay-backdrop';
    el.innerHTML=`<div class="mm-pay-modal"><div class="mm-pay-head"><div><h3>Renew ${c.name||'Consumer'}</h3><div style="color:#7f8b81;font-size:12px">Choose duration and historical month.</div></div><button class="mm-pay-close">×</button></div><div class="mm-pay-periods"><button class="mm-pay-period selected" data-period="full"><b>Full Month</b><small>1st to last day</small><div class="mm-pay-amount">—</div></button><button class="mm-pay-period" data-period="half"><b>Half Month</b><small>1st to 15th</small><div class="mm-pay-amount">—</div></button></div><label>Month<select id="mm-month">${months().map(m=>`<option value="${m.v}">${m.l}</option>`).join('')}</select></label><div id="mm-renew-message"></div><button class="mm-pay-primary" id="mm-renew-save" style="width:100%;margin-top:8px">Renew selected month</button></div>`;
    document.body.appendChild(el);el.querySelector('.mm-pay-close').onclick=closeModal;el.onclick=e=>{if(e.target===el)closeModal()};
    let period='full',monthly=0;
    const msg=t=>el.querySelector('#mm-renew-message').innerHTML=t?'<div class="mm-pay-error">'+t+'</div>':'';
    const infoLoad=async()=>{try{const d=await api('/api/v1/subscriptions/renew-info/'+encodeURIComponent(c.id));monthly=Number(d.monthly_amount||0);el.querySelectorAll('.mm-pay-period .mm-pay-amount')[0].textContent=money(monthly);el.querySelectorAll('.mm-pay-period .mm-pay-amount')[1].textContent=money(monthly/2)}catch(e){msg(e.message)}};
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