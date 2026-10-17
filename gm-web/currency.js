import {escape} from './ui.js';

export function createCurrency({request,cancel,state,render}) {
 let current=null;
 const storageKey='soft-gm-currency-pending-v1';
 let pending={};
 try { pending=JSON.parse(sessionStorage.getItem(storageKey)||'{}'); } catch { pending={}; }
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(pending));}catch{}};
 const button=(text,action,disabled=false)=>`<button type="button" class="button" data-currency="${action}" ${disabled?'disabled':''}>${text}</button>`;
 const notice=error=>error?`<p class="hint-box" role="alert">${escape(error.message)}</p>`:'';
 const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
 function reset(){for(const slot of ['currency-read','currency-log','currency-write'])cancel(slot);current=null;}
 function open(id){
  reset();current={id,balance:null,revision:'',loading:true,error:null,amount:'',direction:'add',reason:'',preview:null,busy:false,result:null,rows:[],page:0,cursors:[''],next:'',logLoading:false,logError:null};
  read();log();
 }
 function read(){const m=current;if(!m)return;m.loading=true;m.error=null;
  request('currency-read',`v1/accounts/${encodeURIComponent(m.id)}/currency`,body=>{if(current!==m)return;m.balance=body.data.balance;m.revision=body.data.revision;m.loading=false;render();},error=>{if(current!==m)return;m.error=error;m.loading=false;render();});
 }
 function log(){const m=current;if(!m)return;m.logLoading=true;m.logError=null;
  const q=new URLSearchParams({limit:'10'});if(m.cursors[m.page])q.set('cursor',m.cursors[m.page]);
  request('currency-log',`v1/accounts/${encodeURIComponent(m.id)}/currency-adjustments?${q}`,body=>{if(current!==m)return;m.rows=body.data;m.next=body.page.nextCursor||'';m.logLoading=false;render();},error=>{if(current!==m)return;m.logError=error;m.logLoading=false;render();});
 }
 function preview(){const m=current;if(!m||m.balance===null||m.loading||pending[m.id])return;
  try{
   if(!/^[1-9][0-9]{0,18}$/.test(m.amount))throw Error('请输入大于 0 的整数点券数量。');
   const amount=BigInt(m.amount)*(m.direction==='subtract'?-1n:1n),after=BigInt(m.balance)+amount;
   if(after<0n||after>9223372036854775807n)throw Error('变更后的余额超出允许范围。');
   if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500)throw Error('请填写操作原因，最多 500 字节。');
   m.preview={currency:'cera',amount:amount.toString(),reason:m.reason,revision:m.revision,before:m.balance,after:after.toString()};m.error=null;
  }catch(e){m.error=e;}render();
 }
 function submit(){const m=current;if(!m||m.busy)return;
  let p=pending[m.id];
  if(!p){if(!m.preview)return;const {before,after,...body}=m.preview;p={key:crypto.randomUUID(),body,before,after};pending[m.id]=p;save();}
  m.busy=true;m.error=null;render();
  request('currency-write',`v1/accounts/${encodeURIComponent(m.id)}/currency-adjustments`,body=>{
   delete pending[m.id];save();if(current!==m)return;
   m.result=body.data;m.balance=body.data.after;m.revision=body.data.revision;m.preview=null;m.amount='';m.reason='';m.busy=false;m.page=0;m.cursors=[''];log();render();
  },error=>{if(current!==m)return;m.busy=false;m.error=error;
   if([400,403,404,409,415,422].includes(error.status)){delete pending[m.id];save();m.preview=null;read();}
   render();
  },{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function view(){const m=current;if(!m)return '';const p=pending[m.id]||m.preview,locked=!!pending[m.id]||m.busy;
  const ready=state.capabilities?.data.features['currency.write'];
  return `<section aria-label="账号点券"><div class="card-head"><div><h2>账号资产</h2><p>点券余额 <strong class="currency-balance">${m.loading?'读取中…':escape(m.balance??'—')}</strong></p></div>${button('刷新余额与流水','refresh',m.busy||!!pending[m.id])}</div>${notice(m.error)}${m.result?`<div class="hint-box" role="status">已${BigInt(m.result.amount)>0n?'增加':'扣减'} ${escape(m.result.amount.replace('-',''))} 点券，余额 ${escape(m.result.after)}。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
  <form id="currency-form" class="field-stack"><fieldset ${locked||m.loading||m.balance===null||!ready?'disabled':''}><div class="currency-fields"><label>操作<select name="direction" aria-label="操作"><option value="add" ${m.direction==='add'?'selected':''}>增加点券</option><option value="subtract" ${m.direction==='subtract'?'selected':''}>扣减点券</option></select></label><label>点券数量<input name="amount" inputmode="numeric" maxlength="19" value="${escape(m.amount)}" required></label><label class="currency-reason">操作原因<input name="reason" maxlength="500" value="${escape(m.reason)}" placeholder="例如：活动补偿、退款扣减" required></label></div><button type="submit" class="button primary">核对变更</button></fieldset></form>
  ${p?`<div class="currency-confirm" aria-label="点券变更确认"><h3>${locked?'待确认的操作':'确认点券变更'}</h3><p>账号 <strong class="mono">${escape(m.id)}</strong></p><dl class="detail-grid"><div><dt>变动点券</dt><dd>${escape(p.body?.amount??p.amount)}</dd></div><div><dt>变更前</dt><dd>${escape(p.before)}</dd></div><div><dt>变更后</dt><dd>${escape(p.after)}</dd></div><div><dt>原因</dt><dd>${escape(p.body?.reason??p.reason)}</dd></div></dl><div class="actions">${button(m.busy?'提交中…':locked?'重试并取得回执':'确认提交','submit',m.busy)}${!locked?button('返回修改','cancel'):''}</div></div>`:''}
  <div class="card-head currency-ledger-head"><h2>GM 点券流水</h2><span>第 ${m.page+1} 页</span></div>${notice(m.logError)}${m.logLoading?'<p class="hint">正在读取流水…</p>':`<div class="table-wrap"><table><thead><tr><th>时间</th><th>变动</th><th>变更前</th><th>变更后</th><th>操作者</th><th>原因</th></tr></thead><tbody>${m.rows.map(r=>`<tr><td>${escape(stamp(r.createdAt))}</td><td><strong>${BigInt(r.amount)>0n?'+':''}${escape(r.amount)}</strong></td><td>${escape(r.before)}</td><td>${escape(r.after)}</td><td>${escape(r.actor)}</td><td>${escape(r.reason)}<small class="mono">${escape(r.requestId)}</small></td></tr>`).join('')||'<tr><td colspan="6">暂无 GM 点券变更记录</td></tr>'}</tbody></table></div>`}<div class="table-footer"><span>本页 ${m.rows.length} 条</span><div class="actions">${button('上一页','prev',m.logLoading||m.page===0)}${button('下一页','next',m.logLoading||!m.next)}</div></div></section>`;
 }
 document.addEventListener('input',e=>{if(current&&e.target.closest('#currency-form')){current[e.target.name]=e.target.value;current.preview=null;}});
 document.addEventListener('change',e=>{if(current&&e.target.closest('#currency-form')){current[e.target.name]=e.target.value;current.preview=null;}});
 document.addEventListener('submit',e=>{if(e.target.id==='currency-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const action=e.target.closest('[data-currency]')?.dataset.currency,m=current;if(!action||!m)return;
  switch(action){case 'submit':submit();break;case 'cancel':m.preview=null;render();break;case 'refresh':m.preview=null;read();m.page=0;m.cursors=[''];log();render();break;case 'prev':if(m.page>0){m.page--;log();render();}break;case 'next':if(m.next){m.cursors=m.cursors.slice(0,m.page+1);m.cursors.push(m.next);m.page++;log();render();}break;}
 });
 return {open,reset,view};
}
