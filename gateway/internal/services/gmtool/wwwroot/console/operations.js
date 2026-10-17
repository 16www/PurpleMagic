import {$,escape} from './ui.js';

const textState={preview:'待确认',running:'进行中',completed:'已完成',partial:'部分失败',pending:'待发放',sent:'已投递',failed:'失败'};
const scopeName=s=>s==='account'?'每账号一次':'每角色一次';
const button=(label,op,id='',disabled=false)=>`<button class="button" data-op="${op}" data-id="${escape(id)}" ${disabled?'disabled':''}>${label}</button>`;
const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
const notice=e=>e?`<div class="hint-box" role="alert">${escape(e.message)}${e.requestId?`<p class="mono">请求编号：${escape(e.requestId)}</p>`:''}</div>`:'';
const reward=r=>`${r.attachments.map(a=>`物品 ${escape(a.itemId)} × ${escape(a.count)}`).join('，')}${r.gold?`${r.attachments.length?' · ':''}${escape(r.gold)} 金币`:''}`;
const newKey=()=>Array.from(crypto.getRandomValues(new Uint8Array(16)),n=>n.toString(16).padStart(2,'0')).join('');
const fresh=()=>({accountId:'',characterId:'',title:'GM 邮件',body:'',gold:0,itemId:'',count:0,scope:'account',autoNewCharacters:false,key:newKey()});
const storageKey='soft-gm-mail-drafts-v1';

export function createOperations({request,cancel,render,state}){
 let drafts;try{drafts=JSON.parse(sessionStorage.getItem(storageKey));}catch{}
 const model={tab:'mail',mail:{...fresh(),...drafts?.mail},rule:{...fresh(),title:'新手福利',...drafts?.rule},busy:false,error:null,message:'',
  benefits:null,tasks:null,selected:null,recipients:null,previewKeys:new Map()};
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify({mail:model.mail,rule:model.rule}));}catch{}};
 const page=()=>({rows:[],loading:false,error:null,cursors:[''],index:0,next:''});
 const active=()=>state.auth==='ready'&&['mail','tasks'].includes(state.route);
 const paint=()=>{if(active())render();};
 function reset(){model.busy=false;model.error=null;model.message='';model.benefits=null;model.tasks=null;model.selected=null;model.recipients=null;}
 function leave(){for(const slot of ['operations-list','operation-task','operation-recipients'])cancel(slot);}
 function loadList(kind,resetPage=false){
  if(!model[kind]||resetPage)model[kind]=page();const list=model[kind];list.loading=true;list.error=null;list.rows=[];paint();
  const q=new URLSearchParams({limit:'20'});if(list.cursors[list.index])q.set('cursor',list.cursors[list.index]);
  request('operations-list',`v1/${kind}?${q}`,body=>{list.rows=body.data;list.next=body.page.nextCursor||'';list.loading=false;paint();},error=>{list.error=error;list.loading=false;paint();});
 }
 function load(){if(state.route==='mail')loadList('benefits');if(state.route==='tasks'){loadList('tasks');if(model.selected)openTask(model.selected.id);}}
 function loadRecipients(resetPage=false){
  const selected=model.selected;if(!selected)return;
  if(!model.recipients||resetPage)model.recipients=page();const list=model.recipients;list.loading=true;list.error=null;list.rows=[];paint();
  const q=new URLSearchParams({limit:'20'});if(list.cursors[list.index])q.set('cursor',list.cursors[list.index]);
  request('operation-recipients',`v1/tasks/${selected.id}/recipients?${q}`,body=>{if(model.selected!==selected)return;list.rows=body.data;list.next=body.page.nextCursor||'';list.loading=false;paint();},error=>{if(model.selected!==selected)return;list.error=error;list.loading=false;paint();});
 }
 function openTask(id){
  cancel('operation-recipients');model.recipients=null;const selected={id,loading:true,data:null,error:null};model.selected=selected;paint();
  request('operation-task',`v1/tasks/${id}`,body=>{if(model.selected!==selected)return;selected.data=body.data;selected.loading=false;loadRecipients(true);},error=>{if(model.selected!==selected)return;selected.error=error;selected.loading=false;paint();});
 }
 function write(path,body,key,success){
  if(model.busy)return;model.busy=true;model.error=null;model.message='';paint();
  request('operation-write',`v1/${path}`,response=>{model.busy=false;success(response.data);paint();},error=>{model.busy=false;model.error=error;paint();},
   {method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body)});
 }
 function previewed(job){model.selected={id:job.id,data:job,loading:false,error:null};model.recipients=null;model.message='预览已保存，请核对收件人与奖励。';if(state.route==='mail'){location.hash='tasks';}else if(state.route==='tasks'){loadList('tasks',true);openTask(job.id);}}
 function paging(kind,list){return `<div class="table-footer"><span>第 ${list.index+1} 页 · 本页 ${list.rows.length} 条</span><div class="actions">${button('上一页',`${kind}-prev`,'',list.loading||list.index===0)}${button('下一页',`${kind}-next`,'',list.loading||!list.next)}</div></div>`;}
 function fields(d,isRule){
  return `<div class="op-fields">${isRule?`<label>发放范围<select name="scope"><option value="account" ${d.scope==='account'?'selected':''}>每账号一次</option><option value="character" ${d.scope==='character'?'selected':''}>每角色一次</option></select></label>`:`<label>账号 ID<input name="accountId" required value="${escape(d.accountId)}" maxlength="128"></label><label>角色 ID<input name="characterId" required value="${escape(d.characterId)}" maxlength="128"></label>`}<label class="op-wide">邮件标题<input name="title" required value="${escape(d.title)}" maxlength="60"></label><label class="op-wide">邮件正文<textarea name="body" rows="3" maxlength="240">${escape(d.body)}</textarea></label><label>物品 ID<input name="itemId" inputmode="numeric" ${isRule?'required':''} value="${escape(d.itemId)}" placeholder="从物品目录选择"></label><label>物品数量<input name="count" type="number" min="0" max="2147483647" step="1" value="${escape(d.count)}" required></label><label>金币<input name="gold" type="number" min="0" max="2147483647" step="1" value="${escape(d.gold)}" required></label>${isRule?`<label class="op-check"><input name="autoNewCharacters" type="checkbox" ${d.autoNewCharacters?'checked':''}>启用后自动发给新建角色</label>`:''}</div>`;
 }
 function rules(){
  const list=model.benefits;if(!list||list.loading)return '<p class="hint">正在读取福利规则…</p>';if(list.error)return notice(list.error);
  return `<div class="table-wrap"><table><thead><tr><th>规则</th><th>发放范围</th><th>奖励</th><th>状态</th><th>操作</th></tr></thead><tbody>${list.rows.map(c=>`<tr><td><strong>${escape(c.reward.title)}</strong><small>${c.autoNewCharacters?'自动覆盖新角色':'手动发放'}</small></td><td>${scopeName(c.scope)}</td><td>${escape(c.itemName)}<small>${reward(c.reward)}</small></td><td><span class="badge ${c.enabled?'good':''}">${c.enabled?'已启用':'已停用'}</span></td><td><div class="actions">${button(c.enabled?'停用':'启用','rule-state',c.id,model.busy)}${button('预览名单','rule-preview',c.id,model.busy)}</div></td></tr>`).join('')||'<tr><td colspan="5">暂无福利规则</td></tr>'}</tbody></table></div>${paging('benefits',list)}`;
 }
 function mail(){
  const isRule=model.tab==='rule',d=isRule?model.rule:model.mail,ready=!!state.capabilities?.data.features[isRule?'benefits.write':'mail.write'];
  return `<div class="page-heading"><h1>邮件与福利</h1><div class="actions">${button('刷新规则','refresh-benefits')}${button('新邮件','new-mail')}</div></div><section class="card"><div class="actions op-tabs">${button('单角色邮件','tab-mail')}${button('新建福利规则','tab-rule')}</div><h2>${isRule?'新建福利规则':'单角色邮件'}</h2>${!ready?'<p class="hint">邮件服务待就绪</p>':''}<form id="op-compose" data-kind="${isRule?'rule':'mail'}"><fieldset ${model.busy||!ready?'disabled':''}>${fields(d,isRule)}<div class="actions"><button class="button primary" type="submit">${model.busy?'正在保存…':isRule?'保存规则':'预览邮件'}</button><a class="button" href="#items">选择物品</a>${!isRule?'<a class="button" href="#players">选择角色</a>':''}</div></fieldset></form></section><section class="card live-selection"><div class="card-head"><h2>福利规则</h2></div>${rules()}</section>`;
 }
 function recipients(){
  const list=model.recipients;if(!list||list.loading)return '<p class="hint">正在读取收件人…</p>';if(list.error)return notice(list.error);
  return `<div class="table-wrap"><table><thead><tr><th>角色 / 账号</th><th>投递状态</th><th>实际收件角色</th><th>邮件回执</th></tr></thead><tbody>${list.rows.map(r=>`<tr><td><strong>${escape(r.name)}</strong><small class="mono">${escape(r.characterId)} / ${escape(r.accountId)}</small></td><td>${textState[r.state]}${r.failure?`<small>${escape(r.failure.message)}</small>`:''}</td><td class="mono">${escape(r.deliveredCharacterId||'—')}</td><td class="mono">${escape(r.mailId||'—')}</td></tr>`).join('')}</tbody></table></div>${paging('recipients',list)}`;
 }
 function taskDetail(){
  const selected=model.selected;if(!selected)return '';if(selected.loading)return '<section class="card live-selection">正在读取任务…</section>';if(selected.error)return `<section class="card live-selection">${notice(selected.error)}${button('刷新任务','refresh-task')}</section>`;
  const j=selected.data;return `<section class="card live-selection" aria-label="任务详情"><div class="card-head"><h2>${escape(j.reward.title)}</h2><div class="actions">${button('刷新任务','refresh-task',j.id,model.busy)}${j.pending?button(j.state==='preview'?'确认发放':'继续发放','confirm-run',j.id,model.busy):''}${j.failed?button('重试失败项','confirm-retry',j.id,model.busy):''}</div></div><dl class="detail-grid"><div><dt>任务状态</dt><dd>${textState[j.state]}</dd></div><div><dt>发放范围</dt><dd>${j.campaignId?scopeName(j.scope):'指定角色'}</dd></div><div><dt>收件人</dt><dd>${j.total} 个 · 待发 ${j.pending} · 成功 ${j.sent} · 失败 ${j.failed}</dd></div><div><dt>奖励</dt><dd>${reward(j.reward)}</dd></div></dl><p>${escape(j.reward.body)}</p><p class="hint mono">任务 ${escape(j.id)} · ${escape(stamp(j.createdAt))}</p>${recipients()}</section>`;
 }
 function tasks(){
  const list=model.tasks;
  let content=!list||list.loading?'<p class="hint">正在读取任务…</p>':list.error?notice(list.error):`<div class="table-wrap"><table><thead><tr><th>任务</th><th>范围</th><th>进度</th><th>状态</th><th>操作</th></tr></thead><tbody>${list.rows.map(j=>`<tr><td><strong>${escape(j.reward.title)}</strong><small>${escape(stamp(j.createdAt))}</small></td><td>${j.campaignId?scopeName(j.scope):'指定角色'}</td><td>${j.sent} / ${j.total}${j.failed?` · ${j.failed} 失败`:''}</td><td>${textState[j.state]}</td><td>${button('查看回执','open-task',j.id)}</td></tr>`).join('')||'<tr><td colspan="5">暂无发放任务</td></tr>'}</tbody></table></div>${paging('tasks',list)}`;
  return `<div class="page-heading"><h1>任务记录</h1>${button('刷新列表','refresh-tasks')}</div><section class="card">${content}</section>${taskDetail()}`;
 }
 function view(){return (model.error?notice(model.error):'')+(model.message?`<p class="hint" role="status">${escape(model.message)}</p>`:'')+(state.route==='mail'?mail():tasks());}
 function confirmRun(retry){
  const j=model.selected?.data;if(!j)return;
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">${retry?'重试失败投递':'确认发放邮件'}</h2>${button('取消','close')}</div><h3>${escape(j.reward.title)}</h3><p>${reward(j.reward)}</p><p>本次最多处理 10 个${retry?'失败':'待发'}目标，共 ${retry?j.failed:j.pending} 个。</p><div class="actions">${button('取消','close')}<button class="button primary" data-op="run" data-retry="${retry}">确认并发放</button></div>`;$('#dialog').showModal();
 }
 function confirmRule(c){
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">${c.enabled?'停用':'启用'}福利规则</h2>${button('取消','close')}</div><h3>${escape(c.reward.title)}</h3><p>${scopeName(c.scope)} · ${reward(c.reward)}</p><p>${c.autoNewCharacters?'启用期间自动发给新建角色。':'通过预览名单手动发放。'}</p><div class="actions"><button class="button primary" data-op="apply-rule-state" data-id="${c.id}">确认${c.enabled?'停用':'启用'}</button></div>`;$('#dialog').showModal();
 }
 function selectCharacter(c){model.mail.accountId=c.accountId;model.mail.characterId=c.characterId;model.mail.key=newKey();model.tab='mail';save();$('#dialog').close();location.hash='mail';}
 function selectItem(i){const d=model.tab==='rule'?model.rule:model.mail;d.itemId=i.itemId;d.count=1;d.key=newKey();save();location.hash='mail';}
 document.addEventListener('input',event=>{
  const form=event.target.closest('#op-compose');if(!form)return;const d=model[form.dataset.kind],el=event.target;
  d[el.name]=el.type==='checkbox'?el.checked:['count','gold'].includes(el.name)?Number(el.value):el.value;d.key=newKey();save();
 });
 document.addEventListener('submit',event=>{
  if(event.target.id!=='op-compose')return;event.preventDefault();const kind=event.target.dataset.kind,d=model[kind];
  const body={title:d.title,body:d.body,gold:d.gold,itemId:d.itemId,count:d.count};
  if(kind==='mail')write('mail/preview',{...body,accountId:d.accountId,characterId:d.characterId},d.key,previewed);
  else write('benefits',{...body,scope:d.scope,autoNewCharacters:d.autoNewCharacters},d.key,c=>{model.message=`已保存“${c.reward.title}”，可在规则列表启用。`;loadList('benefits',true);});
 });
 document.addEventListener('click',event=>{
  const el=event.target.closest('[data-op]');if(!el||el.disabled)return;const {op,id}=el.dataset;
  if(op==='close'){$('#dialog').close();return;}
  if(!active())return;
  if(op==='tab-mail'||op==='tab-rule'){model.tab=op==='tab-rule'?'rule':'mail';paint();}
  else if(op==='new-mail'){model.mail=fresh();model.tab='mail';model.error=null;model.message='';save();paint();}
  else if(op==='refresh-benefits')loadList('benefits');
  else if(op==='refresh-tasks')loadList('tasks');
  else if(op==='open-task'||op==='refresh-task')openTask(id||model.selected.id);
  else if(op==='confirm-run'||op==='confirm-retry')confirmRun(op==='confirm-retry');
  else if(op==='run'){
   const j=model.selected.data;$('#dialog').close();write(`tasks/${j.id}/run`,{revision:j.revision,confirm:true,retry:el.dataset.retry==='true'},null,result=>{model.message=`已投递 ${result.sent} / ${result.total}，失败 ${result.failed}。`;openTask(result.id);loadList('tasks');});
  }else if(op==='rule-state')confirmRule(model.benefits.rows.find(c=>c.id===id));
  else if(op==='apply-rule-state'){
   const c=model.benefits.rows.find(c=>c.id===id);$('#dialog').close();write(`benefits/${id}/state`,{revision:c.revision,enabled:!c.enabled,confirm:true},null,()=>loadList('benefits'));
  }else if(op==='rule-preview'){
   if(!model.previewKeys.has(id))model.previewKeys.set(id,newKey());write(`benefits/${id}/preview`,{},model.previewKeys.get(id),job=>{model.previewKeys.delete(id);previewed(job);});
  }else if(/^(benefits|tasks|recipients)-(prev|next)$/.test(op)){
   const [kind,dir]=op.split('-'),list=model[kind];if(dir==='prev')list.index--;else{list.cursors=list.cursors.slice(0,list.index+1);list.cursors.push(list.next);list.index++;}
   if(kind==='recipients')loadRecipients();else loadList(kind);
  }
 });
 return {view,load,leave,reset,selectCharacter,selectItem};
}
