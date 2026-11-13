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
    for(let i=0;i<24;i++){const x=new Date(d.getFullYear(),d.getMonth()-i,1),v=`${x.getFullYear()}-${String(x.getMonth()+1).padStart(2,'0')}`;out.push({v,l:monthLabel(v)})}
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
      .mm-pay-qr{display:grid;place-items:center;margin:16px 0;padding:16px;border:1px solid #26362a;background:#0a120c;border-radius:18px}.mm-pay-qr img{width:260px;height:260px;object-fit:contain;background:#fff;border-radius:12px;padding:8px}.mm-pay-qr b{margin-top:10px}.mm-pay-qr small{color:#7f8b81;margin-top:3px}
      .mm-pay-error{color:#ff8585;font-size:12px;margin:10px 0}.mm-pay-success{color:#b7ff62;font-size:12px;margin:10px 0}
      .mm-pay-actions{display:flex;gap:9px;flex-wrap:wrap}.mm-pay-actions button{border:0;border-radius:10px;padding:11px 14px;font-weight:800}.mm-pay-primary{background:#b7ff62;color:#10200d}
      @media(max-width:560px){.mm-pay-grid,.mm-pay-periods{grid-template-columns:1fr}.mm-pay-modal{padding:18px}}
    `;document.head.appendChild(s);
  }
  function closeModal(){document.getElementById('mm-pay-backdrop')?.remove()}
  function addOption(select,value,label,selected=false){
    const option=document.createElement('option');option.value=String(value);option.textContent=String(label);option.selected=selected;select.append(option);
  }
  function setMessage(root,message,success=false){
    root.replaceChildren();
    if(!message)return;
    const node=document.createElement('div');node.className=success?'mm-pay-success':'mm-pay-error';node.textContent=String(message);root.append(node);
  }
  function renderQr(root,imageURL,amount,month){
    root.replaceChildren();
    const card=document.createElement('div');card.className='mm-pay-qr';
    const image=new Image();image.alt='Secure payment QR';image.width=260;image.height=260;
    try{
      const parsed=new URL(imageURL,window.location.origin);
      if(parsed.protocol!=='https:' && !(parsed.origin===window.location.origin && parsed.protocol==='http:'))throw new Error('Invalid QR image URL');
      image.src=parsed.href;
    }catch{
      setMessage(document.getElementById('mm-pay-message'),'The payment provider returned an invalid QR image URL.');
      return;
    }
    const amountText=document.createElement('b');amountText.textContent=`${money(amount)} · ${monthLabel(month)}`;
    const note=document.createElement('small');note.textContent='Scan this QR to pay securely';
    const waiting=document.createElement('small');waiting.style.color='#b7ff62';waiting.textContent='Waiting for payment confirmation…';
    card.append(image,amountText,note,waiting);root.append(card);
  }
  function renderPaid(root){
    root.replaceChildren();
    const card=document.createElement('div');card.className='mm-pay-qr';
    const mark=document.createElement('div');mark.style.fontSize='44px';mark.textContent='✓';
    const title=document.createElement('b');title.style.color='#b7ff62';title.textContent='Payment received & subscription renewed';
    const note=document.createElement('small');note.textContent='MessMate verified the payment automatically.';
    card.append(mark,title,note);root.append(card);
  }
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
    el.innerHTML=`<div class="mm-pay-modal">
      <div class="mm-pay-head"><div><h3>Collect payment</h3><div style="color:#7f8b81;font-size:12px;margin-top:4px">Payment is verified automatically. No UTR or owner confirmation is required.</div></div><button class="mm-pay-close" type="button">×</button></div>
      <div class="mm-pay-grid">
        <label>Consumer<select id="mm-consumer"></select></label>
        <label>Month<select id="mm-month"></select></label>
      </div>
      <div class="mm-pay-periods">
        <button type="button" class="mm-pay-period selected" data-period="full"><b>Full Month</b><small>1st to last day</small><div class="mm-pay-amount" id="mm-full">—</div></button>
        <button type="button" class="mm-pay-period" data-period="half"><b>Half Month</b><small>1st to 15th</small><div class="mm-pay-amount" id="mm-half">—</div></button>
      </div>
      <div id="mm-qr-area"></div>
      <div class="mm-pay-actions"><button type="button" class="mm-pay-primary" id="mm-generate">Generate secure QR</button></div>
      <div id="mm-pay-message" aria-live="polite"></div>
    </div>`;
    document.body.appendChild(el);
    el.querySelector('.mm-pay-close').onclick=closeModal;
    el.onclick=e=>{if(e.target===el)closeModal()};
    const consumerSelect=el.querySelector('#mm-consumer');
    for(const c of consumers)addOption(consumerSelect,c.id,`${c.name} — ${c.consumer_id}`);
    const monthSelect=el.querySelector('#mm-month'),monthOptions=months();
    for(let i=0;i<monthOptions.length;i++)addOption(monthSelect,monthOptions[i].v,monthOptions[i].l,i===0);
    if(!consumers.length){
      setMessage(el.querySelector('#mm-pay-message'),'Add a consumer before generating a payment QR.');
      el.querySelector('#mm-generate').disabled=true;return;
    }
    let period='full',monthly=0,pollTimer=null;
    const consumer=()=>consumers.find(x=>x.id===consumerSelect.value);
    const messageRoot=el.querySelector('#mm-pay-message');
    const qrRoot=el.querySelector('#mm-qr-area');
    const loadInfo=async()=>{
      setMessage(messageRoot,'');
      try{
        const d=await api('/api/v1/subscriptions/renew-info/'+encodeURIComponent(consumer().id));
        monthly=Number(d.monthly_amount||0);
        el.querySelector('#mm-full').textContent=money(monthly);
        el.querySelector('#mm-half').textContent=money(monthly/2);
      }catch(e){setMessage(messageRoot,e.message)}
    };
    el.querySelectorAll('.mm-pay-period').forEach(b=>b.onclick=()=>{
      period=b.dataset.period;
      el.querySelectorAll('.mm-pay-period').forEach(x=>x.classList.toggle('selected',x===b));
    });
    consumerSelect.onchange=loadInfo;
    async function pollStatus(intentId){
      clearTimeout(pollTimer);
      try{
        const d=await api('/api/v1/payments/qr/'+encodeURIComponent(intentId)+'/status');
        if(d.status==='paid'){
          renderPaid(qrRoot);setTimeout(()=>window.location.reload(),1200);return;
        }
        if(d.status==='failed'){setMessage(messageRoot,'This payment request could not be completed. Generate a new QR.');return}
      }catch(e){}
      pollTimer=setTimeout(()=>pollStatus(intentId),2500);
    }
    async function generateQR(){
      setMessage(messageRoot,'');
      const amount=period==='half'?monthly/2:monthly;
      if(!amount){setMessage(messageRoot,'Monthly amount is not configured for this consumer.');return}
      const button=el.querySelector('#mm-generate');button.disabled=true;button.textContent='Generating…';
      try{
        const d=await api('/api/v1/payments/qr',{method:'POST',body:JSON.stringify({consumer_id:consumer().id,month:monthSelect.value,period,amount})});
        renderQr(qrRoot,d.image_url,amount,monthSelect.value);
        button.textContent='QR generated';
        pollStatus(d.intent_id);
      }catch(e){setMessage(messageRoot,e.message);button.disabled=false;button.textContent='Generate secure QR'}
    }
    el.querySelector('#mm-generate').onclick=generateQR;
    await loadInfo();
  }
  window.MessMateOpenRazorpayPayment=openPayment;
})();