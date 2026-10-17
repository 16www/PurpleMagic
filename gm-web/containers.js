import {escape} from './ui.js';

export function createContainers({request,cancel,state,render}) {
 let current=null,pending={};
 const storageKey='soft-gm-containers-pending-v1';
 try{const s=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(s&&typeof s==='object'&&!Array.isArray(s))pending=s;}catch{}
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(pending));}catch{}};
 const button=(text,action,disabled=false)=>`<button type="button" class="button" data-capacity="${action}" ${disabled?'disabled':''}>${text}</button>`;
 const notice=e=>e?`<p class="hint-box" role="alert">${escape(e.message)}</p>`:'';
 const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
 function reset(){for(const slot of ['capacity-read','capacity-log','capacity-preview','capacity-write'])cancel(slot);current=null;}
 function open(c){reset();current={id:c.characterId,accountId:c.accountId,name:c.name,scope:JSON.stringify([c.accountId,c.characterId]),data:null,loading:true,error:null,targets:{},reason:'',preview:null,busy:false,result:null,rows:[],page:0,cursors:[''],next:'',logLoading:false,logError:null,epoch:0};read();log();}
 const path=m=>`v1/characters/${encodeURIComponent(m.id)}/`;
 function read(){const m=current;if(!m)return;m.loading=true;m.error=null;
  request('capacity-read',path(m)+'containers',body=>{if(current!==m)return;m.data=body.data;m.targets={};m.loading=false;render();},error=>{if(current!==m)return;m.error=error;m.loading=false;render();});
 }
 function log(){const m=current;if(!m)return;m.logLoading=true;m.logError=null;const q=new URLSearchParams({limit:'10'});if(m.cursors[m.page])q.set('cursor',m.cursors[m.page]);
  request('capacity-log',path(m)+'container-operations?'+q,body=>{if(current!==m)return;m.rows=body.data;m.next=body.page.nextCursor||'';m.logLoading=false;render();},error=>{if(current!==m)return;m.logError=error;m.logLoading=false;render();});
 }
 function preview(){const m=current;if(!m||m.busy||m.loading||!m.data?.initialized||pending[m.scope])return;
  try{if(!Object.keys(m.targets).length)throw Error('请选择至少一项扩容目标。');if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500)throw Error('请填写扩容原因，最多 500 字节。');}catch(e){m.error=e;render();return;}
  const body={accountId:m.accountId,targets:{...m.targets},revision:m.data.revision,reason:m.reason},epoch=++m.epoch;
  m.busy=true;m.error=null;m.preview=null;render();
  request('capacity-preview',path(m)+'container-operations/preview',response=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.preview={body:{...body,planToken:response.data.planToken},receipt:response.data.receipt};render();},error=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.error=error;render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
 }
 function submit(){const m=current;if(!m||m.busy)return;let p=pending[m.scope];if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[m.scope]=p;save();}m.busy=true;m.error=null;render();
  request('capacity-write',path(m)+'container-operations',body=>{delete pending[m.scope];save();if(current!==m)return;m.result=body.data;m.preview=null;m.reason='';m.busy=false;m.page=0;m.cursors=[''];read();log();render();},error=>{if(current!==m)return;m.busy=false;m.error=error;if([400,403,404,409,415,422].includes(error.status)){delete pending[m.scope];save();m.preview=null;}render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function changes(rows){return `<div class="table-wrap"><table><thead><tr><th>容器</th><th>变更前</th><th>变更后</th></tr></thead><tbody>${rows.map(c=>`<tr><td>${escape(c.name)}<small>${escape(c.unit)}</small></td><td>${c.before}</td><td>${c.after}</td></tr>`).join('')}</tbody></table></div>`;}
 function view(){const m=current;if(!m)return '';const p=pending[m.scope]||m.preview,locked=!!pending[m.scope]||m.busy,ready=state.capabilities?.data.features['containers.write'];
  const definitions=m.data?.definitions||[],initialized=m.data?.initialized;
  return `<section aria-label="角色容量"><div class="card-head"><div><h2>背包与仓库容量</h2><p>选择扩容档位，核对变更后提交。</p></div>${button('刷新容量与流水','refresh',locked)}</div>${notice(m.error)}${m.result?`<div class="hint-box" role="status">扩容已完成，共 ${m.result.changes.length} 项。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
  ${m.loading?'<p>正在读取容量…</p>':m.data&&!initialized?'<p class="hint-box">角色尚未创建容器存档，请先进入游戏。</p>':''}
  <form id="capacity-form" class="field-stack"><fieldset ${locked||m.loading||!initialized||!ready?'disabled':''}><div class="capacity-fields">${definitions.map(d=>{const before=m.data.values[d.id],options=d.values.filter(n=>n>before);return `<label>${escape(d.name)}<small>${escape(d.unit)} · 当前 <strong data-capacity-current="${d.id}">${before??'—'}</strong></small><select data-capacity-target="${d.id}" aria-label="${escape(d.name)}" ${options.length?'':'disabled'}><option value="">${options.length?'保持当前':'已达最大档位'}</option>${options.map(n=>`<option value="${n}" ${m.targets[d.id]===n?'selected':''}>${n} ${escape(d.unit==='格'?'格':'扩展格')}</option>`).join('')}</select></label>`;}).join('')}</div><label>扩容原因<input name="reason" aria-label="扩容原因" maxlength="500" value="${escape(m.reason)}" placeholder="例如：活动奖励、容量修复" required></label><p class="hint">主背包和装扮栏显示扩展格数，个人仓库显示总格数。提交时角色需离线。</p><div class="actions">${button('全部扩至最大','maximum')}<button type="submit" class="button primary">预览扩容</button></div></fieldset></form>
  ${p?`<div class="currency-confirm" aria-label="扩容确认"><h3>${locked?'待确认的扩容操作':'确认扩容'}</h3><p>${escape(m.name)} · <span class="mono">${escape(m.id)}</span></p>${changes(p.receipt.changes)}<p>${escape(p.body.reason)}</p><div class="actions">${button(m.busy?'提交中…':locked?'重试扩容并取得回执':'确认扩容提交','submit',m.busy)}${!locked?button('返回修改扩容','cancel'):''}</div></div>`:''}
  <div class="card-head currency-ledger-head"><h2>GM 扩容流水</h2><span>第 ${m.page+1} 页</span></div>${notice(m.logError)}${m.logLoading?'<p>正在读取流水…</p>':`<div class="table-wrap"><table><thead><tr><th>时间</th><th>容量变更</th><th>操作者 / 原因</th></tr></thead><tbody>${m.rows.map(r=>`<tr><td>${escape(stamp(r.createdAt))}</td><td>${r.changes.map(c=>`${escape(c.name)} ${c.before} → ${c.after}`).join('<br>')}</td><td>${escape(r.actor)}<br>${escape(r.reason)}<small class="mono">${escape(r.requestId)}</small></td></tr>`).join('')||'<tr><td colspan="3">暂无 GM 扩容记录</td></tr>'}</tbody></table></div>`}<div class="table-footer"><span>本页 ${m.rows.length} 条</span><div class="actions">${button('上一页扩容流水','prev',m.logLoading||m.page===0)}${button('下一页扩容流水','next',m.logLoading||!m.next)}</div></div></section>`;
 }
 function edit(e){const m=current;if(!m||!e.target.closest('#capacity-form')||m.busy||pending[m.scope])return;if(e.target.dataset.capacityTarget){const id=e.target.dataset.capacityTarget;if(e.target.value==='')delete m.targets[id];else m.targets[id]=Number(e.target.value);}else if(e.target.name==='reason')m.reason=e.target.value;m.preview=null;++m.epoch;document.querySelector('[aria-label="扩容确认"]')?.remove();}
 document.addEventListener('input',edit);document.addEventListener('change',edit);
 document.addEventListener('submit',e=>{if(e.target.id==='capacity-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const action=e.target.closest('[data-capacity]')?.dataset.capacity,m=current;if(!m||!action)return;
  switch(action){case 'submit':submit();break;case 'cancel':m.preview=null;render();break;case 'maximum':if(m.busy||pending[m.scope]||!m.data?.initialized)return;m.targets={};for(const d of m.data.definitions){const max=d.values.at(-1);if(max>m.data.values[d.id])m.targets[d.id]=max;}m.preview=null;render();break;case 'refresh':if(m.busy||pending[m.scope])return;m.preview=null;read();m.page=0;m.cursors=[''];log();render();break;case 'prev':if(!m.logLoading&&m.page>0){m.page--;log();render();}break;case 'next':if(!m.logLoading&&m.next){m.cursors=m.cursors.slice(0,m.page+1);m.cursors.push(m.next);m.page++;log();render();}break;}
 });
 return {open,reset,view};
}
