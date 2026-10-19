import {escape} from './ui.js';

export function createVault({request,cancel,state,render}) {
 let current=null,pending={};
 const storageKey='soft-gm-vault-pending-v1';
 try{const v=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(v&&typeof v==='object'&&!Array.isArray(v))pending=v;}catch{}
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(pending));}catch{}};
 const actions={keep:'保持金币',add:'增加金币',subtract:'扣减金币',set:'设置金币'};
 const button=(text,action,disabled=false)=>`<button type="button" class="button" data-vault="${action}" ${disabled?'disabled':''}>${text}</button>`;
 const notice=e=>e?`<p class="hint-box" role="alert">${escape(e.message)}</p>`:'';
 const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
 const capacity=v=>v.created?v.capacity+' 格':'未开通';
 function reset(){for(const key of ['vault-read','vault-log','vault-preview','vault-write'])cancel(key);current=null;}
 function goldLimit(m){const capacity=m.target==='keep'?m.values?.capacity:Number(m.target);return m.limits?.[capacity]??null;}
 function path(m){return `v1/accounts/${encodeURIComponent(m.id)}/`;}
 function open(character){reset();current={id:character.accountId,values:null,revision:'',capacities:[],loading:true,error:null,action:'keep',amount:'',target:'keep',reason:'',preview:null,busy:false,result:null,rows:[],page:0,cursors:[''],next:'',logLoading:false,logError:null,epoch:0};read();log();}
 function read(){const m=current;if(!m)return;m.loading=true;m.values=null;m.error=null;
  request('vault-read',path(m)+'vault',body=>{if(current!==m)return;const v=body.data;m.values=v.values;m.revision=v.revision;m.maximum=v.maximumGold;m.definitionRevision=v.definition.revision;m.capacities=v.definition.tiers.map(t=>t.capacity);m.limits=Object.fromEntries(v.definition.tiers.map(t=>[t.capacity,t.goldLimit]));m.loading=false;render();},e=>{if(current!==m)return;m.loading=false;m.error=e;render();});
 }
 function log(){const m=current;if(!m)return;m.logLoading=true;m.logError=null;
  const q=new URLSearchParams({limit:'10'});if(m.cursors[m.page])q.set('cursor',m.cursors[m.page]);
  request('vault-log',path(m)+'vault-operations?'+q,body=>{if(current!==m)return;m.rows=body.data;m.next=body.page.nextCursor||'';m.logLoading=false;render();},e=>{if(current!==m)return;m.logLoading=false;m.logError=e;render();});
 }
 function preview(){const m=current;if(!m||m.busy||m.loading||!m.values||pending[m.id])return;
  const body={revision:m.revision,definitionRevision:m.definitionRevision,reason:m.reason};
  try{
   if(m.target!=='keep')body.capacity=Number(m.target);
   if(body.capacity!==undefined&&(!m.capacities.includes(body.capacity)||body.capacity<m.values.capacity))throw Error('请选择当前容量以上的扩容档位。');
   if(m.action!=='keep'){
    if(!/^(0|[1-9][0-9]{0,9})$/.test(m.amount)||(m.action!=='set'&&m.amount==='0'))throw Error('请输入整数金币数量，增加或扣减须大于 0。');
    const amount=BigInt(m.amount),after=m.action==='set'?amount:BigInt(m.values.gold)+(m.action==='subtract'?-amount:amount);
    if(goldLimit(m)===null)throw Error('请选择当前 PVF 支持的金库容量。');
    if(after<0n||after>BigInt(goldLimit(m))||amount>2147483647n)throw Error('变更后的金库金币超出允许范围。');
    body.gold={action:m.action,amount:m.amount};
   }
   if(!body.gold&&body.capacity===undefined)throw Error('请选择金币操作或扩容档位。');
   if(!m.values.created&&body.capacity===undefined)throw Error('请先选择容量开通账号金库。');
   if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500)throw Error('请填写操作原因，最多 500 字节。');
  }catch(e){m.error=e;render();return;}
  const epoch=++m.epoch;m.busy=true;m.error=null;m.preview=null;render();
  request('vault-preview',path(m)+'vault-operations/preview',res=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.preview={body:{...body,planToken:res.data.planToken},receipt:res.data.receipt};render();},e=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.error=e;render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
 }
 function submit(){const m=current;if(!m||m.busy)return;let p=pending[m.id];
  if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[m.id]=p;save();}
  m.busy=true;m.error=null;render();
  request('vault-write',path(m)+'vault-operations',body=>{
   delete pending[m.id];save();if(current!==m)return;m.result=body.data;m.preview=null;m.busy=false;m.amount='';m.action='keep';m.target='keep';m.reason='';m.page=0;m.cursors=[''];read();log();render();
  },e=>{if(current!==m)return;m.busy=false;m.error=e;if([400,403,404,409,415,422].includes(e.status)){delete pending[m.id];save();m.preview=null;}render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function view(){const m=current;if(!m)return '';const p=pending[m.id]||m.preview,locked=!!pending[m.id]||m.busy,ready=state.capabilities?.data.features['vault.write'];
  return `<section aria-label="账号金库管理"><div class="card-head"><div><h2>账号金库</h2><p class="hint">同账号角色共用金币、容量与流水</p></div>${button('刷新金库与流水','refresh',locked)}</div><div class="vault-summary"><div><span>金库金币</span><strong class="vault-balance">${m.loading?'读取中…':escape(m.values?.gold??'—')}</strong></div><div><span>金库容量</span><strong class="vault-capacity">${m.loading?'读取中…':m.values?escape(capacity(m.values)):'—'}</strong></div></div>
  ${notice(m.error)}${m.result?`<div class="hint-box" role="status">金库变更已完成：金币 ${escape(m.result.after.gold)}，容量 ${m.result.after.capacity} 格。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
  <form id="vault-form" class="field-stack"><fieldset ${locked||m.loading||!m.values||!ready?'disabled':''}><div class="capacity-fields"><label>金库金币操作<select name="action" aria-label="金库金币操作">${Object.entries(actions).map(([id,name])=>`<option value="${id}" ${m.action===id?'selected':''}>${name}</option>`).join('')}</select></label><label>${m.action==='set'?'目标金币':'金币数量'}<input name="amount" aria-label="金库金币数量" inputmode="numeric" maxlength="10" value="${escape(m.amount)}" ${m.action==='keep'?'disabled':''}></label><label>金库容量目标<select name="target" aria-label="金库容量目标"><option value="keep" ${m.target==='keep'?'selected':''}>保持容量</option>${m.capacities.filter(n=>n>=(m.values?.capacity||0)).map(n=>`<option value="${n}" ${String(n)===m.target?'selected':''}>${n} 格${!m.values?.created?' · 开通':''}</option>`).join('')}</select></label></div><label>金库操作原因<input name="reason" maxlength="500" value="${escape(m.reason)}" placeholder="例如：活动补偿、金库扩容" required></label><p class="hint">${goldLimit(m)===null?'选择容量后显示金币上限。':'所选容量的金币上限：'+escape(goldLimit(m))+'。'}提交前请退出此账号的游戏连接。</p><div class="actions"><button type="submit" class="button primary">预览金库变更</button>${button('扩至最大容量','maximum')}</div></fieldset></form>
  ${p?`<div class="currency-confirm" aria-label="金库变更确认"><h3>${locked?'待确认的金库操作':p.receipt.before.created?'账号金库变更':'开通账号金库'}</h3><p class="mono">${escape(m.id)}</p><dl class="detail-grid">${[['金币变更前',p.receipt.before.gold],['金币变更后',p.receipt.after.gold],['容量变更前',capacity(p.receipt.before)],['容量变更后',capacity(p.receipt.after)],['原因',p.body.reason]].map(([k,v])=>`<div><dt>${k}</dt><dd>${escape(v)}</dd></div>`).join('')}</dl><div class="actions">${button(m.busy?'提交中…':locked?'重试金库并取得回执':'确认金库提交','submit',m.busy)}${!locked?button('返回修改金库','cancel'):''}</div></div>`:''}
  <div class="card-head currency-ledger-head"><h2>GM 金库流水</h2><span>第 ${m.page+1} 页</span></div>${notice(m.logError)}${m.logLoading?'<p>正在读取流水…</p>':`<div class="table-wrap"><table><thead><tr><th>时间</th><th>金币变化</th><th>容量变化</th><th>操作者 / 原因</th></tr></thead><tbody>${m.rows.map(r=>`<tr><td>${escape(stamp(r.createdAt))}</td><td>${escape(r.before.gold)} → ${escape(r.after.gold)}<small>${BigInt(r.delta)>0n?'+':''}${escape(r.delta)}</small></td><td>${escape(capacity(r.before))} → ${escape(capacity(r.after))}</td><td>${escape(r.actor)}<br>${escape(r.reason)}<small class="mono">${escape(r.requestId)}</small></td></tr>`).join('')||'<tr><td colspan="4">暂无 GM 金库变更记录</td></tr>'}</tbody></table></div>`}<div class="table-footer"><span>本页 ${m.rows.length} 条</span><div class="actions">${button('上一页金库流水','prev',m.logLoading||m.page===0)}${button('下一页金库流水','next',m.logLoading||!m.next)}</div></div></section>`;
 }
 function edit(e){const m=current;if(!m||!e.target.closest('#vault-form')||m.busy||pending[m.id])return;m[e.target.name]=e.target.value;m.preview=null;++m.epoch;if(e.type==='change'&&e.target.tagName==='SELECT')render();else document.querySelector('[aria-label="金库变更确认"]')?.remove();}
 document.addEventListener('input',edit);document.addEventListener('change',edit);
 document.addEventListener('submit',e=>{if(e.target.id==='vault-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const action=e.target.closest('[data-vault]')?.dataset.vault,m=current;if(!action||!m)return;
  switch(action){case 'submit':submit();break;case 'cancel':m.preview=null;render();break;case 'maximum':if(m.busy||pending[m.id]||!m.values)return;m.target=String(m.capacities.at(-1));m.preview=null;render();break;case 'refresh':if(m.busy||pending[m.id])return;m.preview=null;read();m.page=0;m.cursors=[''];log();render();break;case 'prev':if(!m.logLoading&&m.page>0){m.page--;log();render();}break;case 'next':if(!m.logLoading&&m.next){m.cursors=m.cursors.slice(0,m.page+1);m.cursors.push(m.next);m.page++;log();render();}break;}
 });
 return {open,reset,view};
}
