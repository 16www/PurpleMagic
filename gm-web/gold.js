import {escape} from './ui.js';

export function createGold({request,cancel,state,render}) {
 let current=null;
 const storageKey='soft-gm-gold-pending-v1';
 let pending={};
 try { const stored=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(stored&&typeof stored==='object'&&!Array.isArray(stored))pending=stored; } catch {}
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(pending));}catch{}};
 const button=(text,action,disabled=false)=>`<button type="button" class="button" data-gold="${action}" ${disabled?'disabled':''}>${text}</button>`;
 const notice=e=>e?`<p class="hint-box" role="alert">${escape(e.message)}</p>`:'';
 const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
 const actions={add:'增加金币',subtract:'扣减金币',set:'设置金币'};
 function reset(){for(const slot of ['gold-read','gold-log','gold-preview','gold-write'])cancel(slot);current=null;}
 function open(c){reset();current={id:c.characterId,accountId:c.accountId,name:c.name,scope:JSON.stringify([c.accountId,c.characterId]),balance:null,revision:'',loading:true,error:null,amount:'',action:'add',reason:'',preview:null,busy:false,result:null,rows:[],page:0,cursors:[''],next:'',logLoading:false,logError:null,epoch:0};read();log();}
 function path(m){return `v1/characters/${encodeURIComponent(m.id)}/`;}
 function read(){const m=current;if(!m)return;m.loading=true;m.error=null;
  request('gold-read',path(m)+'gold',body=>{if(current!==m)return;m.balance=body.data.balance;m.maximum=body.data.maximum;m.revision=body.data.revision;m.loading=false;render();},error=>{if(current!==m)return;m.error=error;m.loading=false;render();});
 }
 function log(){const m=current;if(!m)return;m.logLoading=true;m.logError=null;
  const q=new URLSearchParams({limit:'10'});if(m.cursors[m.page])q.set('cursor',m.cursors[m.page]);
  request('gold-log',path(m)+'gold-operations?'+q,body=>{if(current!==m)return;m.rows=body.data;m.next=body.page.nextCursor||'';m.logLoading=false;render();},error=>{if(current!==m)return;m.logError=error;m.logLoading=false;render();});
 }
 function preview(){const m=current;if(!m||m.busy||m.loading||m.balance===null||pending[m.scope])return;
  try{
   if(!/^(0|[1-9][0-9]{0,9})$/.test(m.amount)||(m.action!=='set'&&m.amount==='0'))throw Error('请输入整数金币数量，增加或扣减须大于 0。');
   const amount=BigInt(m.amount),after=m.action==='set'?amount:BigInt(m.balance)+(m.action==='subtract'?-amount:amount);
   if(after<0n||after>BigInt(m.maximum)||amount>BigInt(m.maximum))throw Error('变更后的金币超出允许范围。');
   if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500)throw Error('请填写操作原因，最多 500 字节。');
  }catch(e){m.error=e;render();return;}
  const body={accountId:m.accountId,action:m.action,amount:m.amount,revision:m.revision,reason:m.reason},epoch=++m.epoch;
  m.busy=true;m.error=null;m.preview=null;render();
  request('gold-preview',path(m)+'gold-operations/preview',response=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.preview={body:{...body,planToken:response.data.planToken},receipt:response.data.receipt};render();},error=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.error=error;render();}, {method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
 }
 function submit(){const m=current;if(!m||m.busy)return;let p=pending[m.scope];
  if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[m.scope]=p;save();}
  m.busy=true;m.error=null;render();
  request('gold-write',path(m)+'gold-operations',body=>{
   delete pending[m.scope];save();if(current!==m)return;m.result=body.data;m.preview=null;m.amount='';m.reason='';m.busy=false;m.page=0;m.cursors=[''];read();log();render();
  },error=>{if(current!==m)return;m.busy=false;m.error=error;
   if([400,403,404,409,415,422].includes(error.status)){delete pending[m.scope];save();m.preview=null;}
   render();
  },{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function view(){const m=current;if(!m)return '';const p=pending[m.scope]||m.preview,locked=!!pending[m.scope]||m.busy,ready=state.capabilities?.data.features['gold.write'];
  return `<section aria-label="角色金币"><div class="card-head"><div><h2>角色金币</h2><p>当前金币 <strong class="currency-balance gold-balance">${m.loading?'读取中…':escape(m.balance??'—')}</strong></p></div>${button('刷新金币与流水','refresh',locked)}</div>${notice(m.error)}${m.result?`<div class="hint-box" role="status">${escape(actions[m.result.action])}已完成，金币 ${escape(m.result.after)}。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
  <form id="gold-form" class="field-stack"><fieldset ${locked||m.loading||m.balance===null||!ready?'disabled':''}><div class="currency-fields"><label>金币操作<select name="action" aria-label="金币操作">${Object.entries(actions).map(([id,name])=>`<option value="${id}" ${m.action===id?'selected':''}>${name}</option>`).join('')}</select></label><label>${m.action==='set'?'目标金币':'金币数量'}<input name="amount" aria-label="金币数量" inputmode="numeric" maxlength="10" value="${escape(m.amount)}" required></label><label class="currency-reason">金币操作原因<input name="reason" maxlength="500" value="${escape(m.reason)}" placeholder="例如：活动补偿、异常余额调整" required></label></div><p class="hint">角色金币范围：0–${escape(m.maximum||'2147483647')}。提交时角色需离线。</p><button type="submit" class="button primary">预览金币变更</button></fieldset></form>
  ${p?`<div class="currency-confirm" aria-label="金币变更确认"><h3>${locked?'待确认的金币操作':escape(actions[p.body.action])}</h3><p>${escape(m.name)} · <span class="mono">${escape(m.id)}</span></p><dl class="detail-grid">${[['变更前',p.receipt.before],['变更后',p.receipt.after],['变动金币',p.receipt.delta],['原因',p.body.reason]].map(([k,v])=>`<div><dt>${k}</dt><dd>${escape(v)}</dd></div>`).join('')}</dl><div class="actions">${button(m.busy?'提交中…':locked?'重试金币并取得回执':'确认金币提交','submit',m.busy)}${!locked?button('返回修改金币','cancel'):''}</div></div>`:''}
  <div class="card-head currency-ledger-head"><h2>GM 金币流水</h2><span>第 ${m.page+1} 页</span></div>${notice(m.logError)}${m.logLoading?'<p class="hint">正在读取流水…</p>':`<div class="table-wrap"><table><thead><tr><th>时间</th><th>操作</th><th>变动</th><th>变更前</th><th>变更后</th><th>操作者 / 原因</th></tr></thead><tbody>${m.rows.map(r=>`<tr><td>${escape(stamp(r.createdAt))}</td><td>${escape(actions[r.action])}</td><td>${BigInt(r.delta)>0n?'+':''}${escape(r.delta)}</td><td>${escape(r.before)}</td><td>${escape(r.after)}</td><td>${escape(r.actor)}<br>${escape(r.reason)}<small class="mono">${escape(r.requestId)}</small></td></tr>`).join('')||'<tr><td colspan="6">暂无 GM 金币变更记录</td></tr>'}</tbody></table></div>`}<div class="table-footer"><span>本页 ${m.rows.length} 条</span><div class="actions">${button('上一页金币流水','prev',m.logLoading||m.page===0)}${button('下一页金币流水','next',m.logLoading||!m.next)}</div></div></section>`;
 }
 function edit(e){const m=current;if(!m||!e.target.closest('#gold-form')||m.busy||pending[m.scope])return;m[e.target.name]=e.target.value;m.preview=null;++m.epoch;if(e.type==='change'&&e.target.tagName==='SELECT')render();else{const old=document.querySelector('[aria-label="金币变更确认"]');if(old)old.remove();}}
 document.addEventListener('input',edit);document.addEventListener('change',edit);
 document.addEventListener('submit',e=>{if(e.target.id==='gold-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const action=e.target.closest('[data-gold]')?.dataset.gold,m=current;if(!action||!m)return;
  switch(action){case 'submit':submit();break;case 'cancel':m.preview=null;render();break;case 'refresh':if(m.busy||pending[m.scope])return;m.preview=null;read();m.page=0;m.cursors=[''];log();render();break;case 'prev':if(!m.logLoading&&m.page>0){m.page--;log();render();}break;case 'next':if(!m.logLoading&&m.next){m.cursors=m.cursors.slice(0,m.page+1);m.cursors.push(m.next);m.page++;log();render();}break;}
 });
 return {open,reset,view};
}
