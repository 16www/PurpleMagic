import {escape} from './ui.js';
import {createMailboxCleanup} from './mailbox-cleanup.js';

const button=(label,action,disabled=false)=>`<button type="button" class="button" data-mail-operation="${action}" ${disabled?'disabled':''}>${label}</button>`;
const notice=e=>e?`<div class="hint-box" role="alert">${escape(e.message)}</div>`:'';
export function mailboxChangesView(changes){
 return `<div class="table-wrap mailbox-changes"><table><thead><tr><th>邮件</th><th>领取状态</th><th>原金币 / 附件</th><th>删除影响</th></tr></thead><tbody>${changes.map(c=>`<tr><td data-label="邮件">${escape(c.title)}<small class="mono">${escape(c.mailId)}</small></td><td data-label="领取状态">${c.claimed?'已领取':'未领取'}</td><td data-label="原金币 / 附件">金币 ${escape(c.gold)}${c.attachments.map(a=>`<small>物品 ${escape(a.item_id)} × ${escape(a.count)}${a.bind?' · 已绑定':''}</small>`).join('')}</td><td data-label="删除影响">${c.discardedAssets?'删除邮件并丢弃未领取资产':'删除邮件'}</td></tr>`).join('')}</tbody></table></div>`;
}
export function createMailboxOperations({request,cancel,state,render,refresh}){
 const cleanup=createMailboxCleanup({request,cancel,state,render,refresh,changesView:mailboxChangesView});
 const storageKey='soft-gm-mailbox-pending-v1';let pending={};
 try{const v=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(v&&typeof v==='object'&&!Array.isArray(v))pending=v;}catch{}
 const save=()=>sessionStorage.setItem(storageKey,JSON.stringify(pending));
 const model=()=>state.detail?.resources?state.detail.resources.mailOperation??={selected:new Set(),reason:'',discardUnclaimed:false,busy:false,error:null,preview:null,result:null,epoch:0}:null;
 const identity=()=>state.detail?.data?.character;
 const scope=c=>JSON.stringify([c.accountId,c.characterId]);
 function locked(){const m=model(),c=identity();return !!(m?.busy||cleanup.busy()||(c&&pending[scope(c)]));}
 function reset(){cleanup.reset();cancel('mail-preview');cancel('mail-write');const m=model();if(m){m.busy=false;m.preview=null;m.epoch++;}}
 function clearSelection(){cancel('mail-preview');const m=model();if(m){m.selected.clear();m.preview=null;m.epoch++;if(!pending[scope(identity())])m.busy=false;}}
 function checkbox(row){const m=model();return `<input type="checkbox" data-mail-select="${escape(row.mailId)}" aria-label="选择邮件 ${escape(row.mailId)}" ${m?.selected.has(row.mailId)?'checked':''} ${locked()?'disabled':''}>`;}
 function preview(){const owner=state.detail,m=model(),c=identity();if(!c||!m||locked())return;
  if(!m.selected.size||m.selected.size>100){m.error=Error('请选择1至100封邮件。');render();return;}
  if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500){m.error=Error('请填写删除原因，最多 500 字节。');render();return;}
  const body={accountId:c.accountId,items:[...m.selected].map(mailId=>({mailId})),discardUnclaimed:m.discardUnclaimed,reason:m.reason};const epoch=++m.epoch;m.busy=true;m.error=null;m.result=null;render();
  request('mail-preview',`v1/characters/${encodeURIComponent(c.characterId)}/mailbox-operations/preview`,res=>{if(state.detail!==owner||m.epoch!==epoch)return;m.busy=false;m.preview={body:{...body,items:res.data.items,planToken:res.data.planToken},receipt:res.data.receipt};render();},e=>{if(state.detail!==owner||m.epoch!==epoch)return;m.busy=false;m.error=e;render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
 }
 function submit(){const owner=state.detail,m=model(),c=identity();if(!c||!m||m.busy)return;const id=scope(c);let p=pending[id];
  if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[id]=p;try{save();}catch{delete pending[id];m.error=Error('浏览器无法保存请求编号，请启用会话存储后重试。');render();return;}}
  m.busy=true;m.error=null;render();
  request('mail-write',`v1/characters/${encodeURIComponent(c.characterId)}/mailbox-operations`,res=>{delete pending[id];try{save();}catch{}if(state.detail!==owner)return;m.busy=false;m.preview=null;m.selected.clear();m.result=res.data;refresh();render();},e=>{if(state.detail!==owner)return;m.busy=false;m.error=e;if([400,403,404,409,415,422].includes(e.status)){delete pending[id];try{save();}catch{}m.preview=null;}render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function view(){const m=model(),c=identity();if(!m||!c)return '';const p=pending[scope(c)]||m.preview,waiting=!!pending[scope(c)],ready=state.capabilities?.data.features['mailbox.write'];
  return `<section class="mailbox-operations" aria-label="邮箱删除操作"><div class="card-head"><h3>删除邮件</h3><span>已选 ${m.selected.size} 封</span></div>${notice(m.error)}${m.result?`<div class="hint-box" role="status">已删除 ${m.result.changes.length} 封邮件。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}<form id="mail-operation-form"><fieldset ${locked()||!ready?'disabled':''}><label>邮件删除原因<input name="reason" maxlength="500" required value="${escape(m.reason)}" placeholder="例如：清理已领取邮件、撤回错误投递"></label><label class="mail-discard"><input type="checkbox" name="discardUnclaimed" ${m.discardUnclaimed?'checked':''}>同时丢弃未领取资产</label><div class="actions"><button type="submit" class="button primary">预览邮件删除</button>${button('选择本页邮件','select-page')}${button('选择本页已领取','select-claimed')}${button('清空邮件选择','clear')}${button('预览跨页清理','range')}</div></fieldset></form>${p?`<div class="mail-confirm" aria-label="邮件删除确认"><h3>${waiting?'待确认的邮件删除':'确认删除邮件'}</h3><p>${escape(c.name)} · ${escape(p.body.reason)}</p>${mailboxChangesView(p.receipt.changes)}<div class="actions">${button(m.busy?'提交中…':waiting?'重试并取得邮件回执':'确认删除邮件','submit',m.busy)}${!waiting?button('返回修改邮件操作','cancel',m.busy):''}</div></div>`:''}</section>${cleanup.view()}`;
 }
 function edit(e){const m=model();if(!m||locked())return;
  if(e.target.matches('[data-mail-select]')){const id=e.target.dataset.mailSelect;e.target.checked?m.selected.add(id):m.selected.delete(id);m.preview=null;m.error=null;m.epoch++;render();}
  else if(e.target.closest('#mail-operation-form')){m[e.target.name]=e.target.type==='checkbox'?e.target.checked:e.target.value;m.preview=null;m.epoch++;document.querySelector('[aria-label="邮件删除确认"]')?.remove();}
 }
 document.addEventListener('change',edit);document.addEventListener('input',e=>{if(e.target.type!=='checkbox')edit(e);});
 document.addEventListener('submit',e=>{if(e.target.id==='mail-operation-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const control=e.target.closest('[data-mail-operation], [data-mail-delete]'),m=model();if(!control||!m)return;const action=control.dataset.mailOperation;
  if(action==='submit'){submit();return;}if(locked())return;if(action==='range'){cleanup.create(m.reason,m.discardUnclaimed);return;}
  if(control.dataset.mailDelete!==undefined){m.selected=new Set([control.dataset.mailDelete]);m.preview=null;render();document.querySelector('#mail-operation-form input[name=reason]')?.focus();return;}
  if(action==='select-page'||action==='select-claimed')m.selected=new Set(state.detail.resources.rows.filter(r=>action==='select-page'||r.claimed).map(r=>r.mailId));
  if(action==='clear')m.selected.clear();m.preview=null;render();
 });
 return {reset,clearSelection,checkbox,view};
}
