import {$,escape} from './ui.js';
import {createBenefitPVF} from './benefit-pvf.js';

const textState={preview:'待确认',running:'进行中',completed:'已完成',partial:'部分失败',pending:'待发放',sent:'已投递',failed:'失败'};
const scopeName=s=>s==='account'?'每账号一次':'每角色一次';
const recipientScope=j=>j.recipientSource?.kind==='online'?'预览时在线角色':j.campaignId?scopeName(j.scope):'指定角色';
const button=(label,op,id='',disabled=false)=>`<button type="button" class="button" data-op="${op}" data-id="${escape(id)}" ${disabled?'disabled':''}>${label}</button>`;
const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
const notice=e=>e?`<div class="hint-box" role="alert">${escape(e.message)}${e.requestId?`<p class="mono">请求编号：${escape(e.requestId)}</p>`:''}</div>`:'';
const reward=r=>`${r.attachments.map(a=>{const o=a.options||{};return `物品 ${escape(a.itemId)} × ${escape(a.count)}${o.reinforce?` · 强化/增幅 +${escape(o.reinforce)}`:''}${o.amplifyType?` · ${['','体力','精神','力量','智力'][Number(o.amplifyType)]||'增幅'} ${escape(o.amplifyValue||'')}`:''}${o.forgingLevel?` · 锻造 ${escape(o.forgingLevel)}`:''}${o.qualitySeed?` · ${String(o.qualitySeed)==='999999998'?'100% 最上级':'随机品级（已固定）'}`:''}${a.expiresAt?` · 到期 ${escape(stamp(a.expiresAt))}`:''}`;}).join('，')}${r.gold?`${r.attachments.length?' · ':''}${escape(r.gold)} 金币`:''}${r.deliveryPlan?.options.equipmentQuality==='random'?' · 每位收件人投递时随机品级':''}${r.deliveryPlan?.options.expireDays?` · 有效 ${escape(r.deliveryPlan.options.expireDays)} 天（从投递时起）`:''}`;
const newKey=()=>Array.from(crypto.getRandomValues(new Uint8Array(16)),n=>n.toString(16).padStart(2,'0')).join('');
const fresh=()=>({accountId:'',characterId:'',recipientMode:'single',recipients:[],accounts:[],options:{},pvfChecksum:'',title:'GM 邮件',body:'',gold:0,itemId:'',count:0,scope:'account',autoNewCharacters:false,key:newKey()});
const storageKey='soft-gm-mail-drafts-v1';

export function createOperations({request,cancel,render,state}){
 let drafts;try{drafts=JSON.parse(sessionStorage.getItem(storageKey));}catch{}
 const model={tab:'mail',mail:{...fresh(),...drafts?.mail},rule:{...fresh(),title:'新手福利',...drafts?.rule},busy:false,error:null,message:'',
  benefits:null,tasks:null,selected:null,recipients:null,grant:null,run:null,previewKeys:new Map()};
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify({mail:model.mail,rule:model.rule}));}catch{}};
 const page=()=>({rows:[],loading:false,error:null,cursors:[''],index:0,next:''});
 const active=()=>state.auth==='ready'&&['mail','tasks'].includes(state.route);
 const pvf=createBenefitPVF({request,cancel,state,reload:()=>{if(state.route==='mail')loadList('benefits');}});
 const paint=()=>{if(active())render();};
 function reset(){model.run=null;model.busy=false;model.error=null;model.message='';model.benefits=null;model.tasks=null;model.selected=null;model.recipients=null;}
 function leave(){model.run=null;pvf.leave();for(const slot of ['operations-list','operation-task','operation-recipients','confirmation-recipients','scheduled-task'])cancel(slot);}
 function loadList(kind,resetPage=false){
  if(!model[kind]||resetPage)model[kind]=page();const list=model[kind];list.loading=true;list.error=null;list.rows=[];paint();
  const q=new URLSearchParams({limit:'20'});if(list.cursors[list.index])q.set('cursor',list.cursors[list.index]);
  request('operations-list',`v1/${kind}?${q}`,body=>{list.rows=body.data;list.next=body.page.nextCursor||'';list.loading=false;paint();},error=>{list.error=error;list.loading=false;paint();});
 }
 function load(){if(state.route==='mail'){loadList('benefits');if(model[model.tab].itemId)loadGrant();}if(state.route==='tasks'){loadList('tasks');if(model.selected)openTask(model.selected.id);}}
 function loadGrant(){
  const kind=model.tab,d=model[kind];if(!d.itemId)return;const id=d.itemId,g={loading:true,itemId:id,kind};model.grant=g;paint();
  request('mail-grant',`v1/items/${encodeURIComponent(id)}/grant-options`,body=>{if(model.grant!==g||id!==d.itemId)return;model.grant={data:body.data,itemId:id,kind};if(!d.pvfChecksum){d.pvfChecksum=body.data.pvfChecksum;d.key=newKey();save();}paint();},error=>{if(model.grant!==g||id!==d.itemId)return;model.grant={error,itemId:id,kind};paint();});
 }
 function addRecipient(c){
  const d=model.mail;if(!c.accountId||!c.characterId){model.error={message:'请填写账号与角色编号'};return;}
  if(d.recipients.some(r=>r.characterId===c.characterId)){model.error={message:'角色已在收件名单中'};return;}
  if(d.recipients.length>=100){model.error={message:'每份名单最多100个角色'};return;}
  d.recipients.push({accountId:c.accountId,characterId:c.characterId,name:c.name||''});d.accountId='';d.characterId='';d.key=newKey();model.error=null;save();
 }
 function mailTargets(d){
  const modes=`<div class="actions">${button('单个角色','recipient-single')}${button('指定名单','recipient-selected')}${button('在线角色','recipient-online')}</div>`;
  if(d.recipientMode==='online')return `<div class="op-wide">${modes}<p class="hint">保存预览时获取全部在线角色。名单固定后，角色离线仍会收到邮件；后续上线的角色加入下一次发放。</p><div class="actions">${button('新建在线发放','new-online')}</div></div>`;
  const many=d.recipientMode==='selected';
  return `<div class="op-wide">${modes}${many?`<p>已选 ${d.accounts.length} 个账号（全部现有角色）、${d.recipients.length} 个角色；重叠角色只发一次。</p><ul class="mail-targets">${d.accounts.map(a=>`<li><span>账号 ${escape(a.name||a.accountId)}</span>${button("移除","account-remove",a.accountId)}</li>`).join("")}</ul><ul class="mail-targets">${d.recipients.map(r=>`<li><span>${escape(r.name||r.characterId)} <small>${escape(r.accountId)} / ${escape(r.characterId)}</small></span>${button('移除','recipient-remove',r.characterId)}</li>`).join('')}</ul>`:''}</div><label>账号 ID<input name="accountId" ${many?'':'required'} value="${escape(d.accountId)}" maxlength="128"></label><label>角色 ID<input name="characterId" ${many?'':'required'} value="${escape(d.characterId)}" maxlength="5"></label>${many?`<div class="op-wide actions">${button('加入名单','recipient-add')}<a class="button" href="#players">从账号列表添加</a></div>`:''}`;
 }
 function grantFields(d){
  if(!d.itemId)return '<p class="hint op-wide">只发金币时，物品 ID 留空、物品数量填 0。</p>';
  const g=model.grant;
  const heading=`<div class="op-wide actions"><strong>附件参数</strong>${button('读取发放参数','grant-load','',!d.itemId||g?.loading)}</div>`;
  if(g?.loading)return heading+'<p class="hint op-wide">正在读取物品参数…</p>';
  if(!g?.data||g.itemId!==d.itemId||g.kind!==model.tab)return heading+`<div class="op-wide">${notice(g?.error)}</div>`;
  const c=g.data,e=c.equipment,o=d.options;
  const number=(label,name,max)=>`<label>${label}<input name="grant.${name}" type="number" min="0" max="${max}" step="1" value="${escape(o[name]||0)}"></label>`;
  return heading+(d.pvfChecksum&&d.pvfChecksum!==c.pvfChecksum?'<p class="hint op-wide">物品版本已更新。已有预览可继续读回；重新选择物品后可准备新任务。</p>':'')+`<p class="hint op-wide">${escape(c.name)} · ${c.kind==='equipment'?'每封一件装备':'每封按填写数量发放'}</p>`+
   (e.supportsQuality?`<label>装备品级<select name="grant.equipmentQuality" aria-label="装备品级">${e.qualityOptions.map(v=>`<option value="${escape(v.value)}" ${v.value===(o.equipmentQuality||'default')?'selected':''}>${escape(v.label)}</option>`).join('')}</select></label>`:'')+
   (e.canUpgrade?number('强化 / 增幅等级','reinforce',e.maxUpgradeLevel):'')+
   (e.canAmplify?`<label>增幅属性<select name="grant.amplifyType" aria-label="增幅属性">${['无','体力','精神','力量','智力'].map((v,i)=>`<option value="${i}" ${i===Number(o.amplifyType||0)?'selected':''}>${v}</option>`).join('')}</select></label>`:'')+
   (e.canForge?number('锻造等级','forgingLevel',e.maxForgingLevel):'')+
   number('有效天数','expireDays',c.expiration.maxDays)+`<p class="hint op-wide">有效天数为0时使用物品默认期限；指定天数从${model.tab==='rule'?'实际投递':'保存预览'}时开始计算。</p>`;
 }
 function loadRecipients(resetPage=false){
  const selected=model.selected;if(!selected)return;
  if(!model.recipients||resetPage)model.recipients=page();const list=model.recipients;list.loading=true;list.error=null;list.rows=[];paint();
  const q=new URLSearchParams({limit:'20'});if(list.cursors[list.index])q.set('cursor',list.cursors[list.index]);
  request('operation-recipients',`v1/tasks/${selected.id}/recipients?${q}`,body=>{if(model.selected!==selected)return;list.rows=body.data;list.next=body.page.nextCursor||'';list.loading=false;paint();},error=>{if(model.selected!==selected)return;list.error=error;list.loading=false;paint();});
 }
 function openTask(id){
  if(model.run?.id!==id)model.run=null;
  cancel('operation-recipients');model.recipients=null;const selected={id,loading:true,data:null,error:null};model.selected=selected;paint();
  request('operation-task',`v1/tasks/${id}`,body=>{if(model.selected!==selected)return;selected.data=body.data;selected.loading=false;loadRecipients(true);if(body.data.schedule==='active')watchScheduled(body.data);},error=>{if(model.selected!==selected)return;selected.error=error;selected.loading=false;paint();});
 }
 function write(path,body,key,success,failure){
  if(model.busy)return;model.busy=true;model.error=null;model.message='';paint();
  request('operation-write',`v1/${path}`,response=>{model.busy=false;success(response.data);paint();},error=>{model.busy=false;model.error=error;failure?.(error);paint();},
   {method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body)});
 }
 function previewed(job){model.selected={id:job.id,data:job,loading:false,error:null};model.recipients=null;model.message='';confirmRun(false);}
 function paging(kind,list){return `<div class="table-footer"><span>第 ${list.index+1} 页 · 本页 ${list.rows.length} 条</span><div class="actions">${button('上一页',`${kind}-prev`,'',list.loading||list.index===0)}${button('下一页',`${kind}-next`,'',list.loading||!list.next)}</div></div>`;}
 function fields(d,isRule){
  return `<div class="op-fields">${isRule?`<label>发放范围<select name="scope"><option value="account" ${d.scope==='account'?'selected':''}>每账号一次</option><option value="character" ${d.scope==='character'?'selected':''}>每角色一次</option></select></label><p class="hint op-wide">启用后自动向全部现有角色发放，按所选范围去重。</p>`:mailTargets(d)}<label class="op-wide">邮件标题<input name="title" required value="${escape(d.title)}" maxlength="60"></label><label class="op-wide">邮件正文<textarea name="body" rows="3" maxlength="240">${escape(d.body)}</textarea></label><label>物品 ID<input name="itemId" inputmode="numeric" value="${escape(d.itemId)}" placeholder="从物品目录选择"></label><label>物品数量<input name="count" type="number" min="0" max="2147483647" step="1" value="${escape(d.count)}" required></label><label>金币<input name="gold" type="number" min="0" max="2147483647" step="1" value="${escape(d.gold)}" required></label>${isRule?`<label class="op-check"><input name="autoNewCharacters" type="checkbox" ${d.autoNewCharacters?'checked':''}>包含以后新建的角色</label>`:''}<div class="op-fields op-wide" id="op-grant-fields">${grantFields(d)}</div></div>`;
 }
 function rules(){
  const list=model.benefits;if(!list||list.loading)return '<p class="hint">正在读取福利规则…</p>';if(list.error)return notice(list.error);
  return `<div class="table-wrap"><table><thead><tr><th>规则</th><th>发放范围</th><th>奖励</th><th>状态</th><th>操作</th></tr></thead><tbody>${list.rows.map(c=>`<tr><td><strong>${escape(c.reward.title)}</strong><small>${c.autoExistingCharacters?(c.autoNewCharacters?'现有及未来角色自动发放':'现有角色自动发放'):(c.autoNewCharacters?'未来角色自动发放':'待启用自动发放')}</small></td><td>${scopeName(c.scope)}</td><td>${escape(c.itemName)}<small>${reward(c.reward)}</small></td><td><span class="badge ${c.enabled?'good':''}">${c.enabled?'已启用':'已停用'}</span></td><td><div class="actions">${button(c.enabled?'停用':'启用','rule-state',c.id,model.busy)}${c.reward.deliveryPlan?button('核对 PVF','rule-pvf',c.id,model.busy)+button('核对历史','rule-pvf-history',c.id,model.busy):''}</div></td></tr>`).join('')||'<tr><td colspan="5">暂无福利规则</td></tr>'}</tbody></table></div>${paging('benefits',list)}`;
 }
 function mail(){
  const isRule=model.tab==='rule',d=isRule?model.rule:model.mail,ready=!!state.capabilities?.data.features[isRule?'benefits.write':'mail.write'];
  return `<div class="page-heading"><h1>邮件与福利</h1><div class="actions">${button('全部角色福利','all-characters-benefit','',model.busy)}${button('刷新规则','refresh-benefits')}${button('新邮件','new-mail')}</div></div><section class="card"><div class="actions op-tabs">${button('发送邮件','tab-mail')}${button('新建福利规则','tab-rule')}</div><h2>${isRule?'新建福利规则':'发送邮件'}</h2>${!ready?'<p class="hint">邮件服务待就绪</p>':''}<form id="op-compose" data-kind="${isRule?'rule':'mail'}"><fieldset ${model.busy||!ready?'disabled':''}>${fields(d,isRule)}<div class="actions"><button class="button primary" type="submit">${model.busy?'正在保存…':isRule?'保存规则':'发送道具'}</button><a class="button" href="#items">选择物品</a>${!isRule?'<a class="button" href="#players">选择角色</a>':''}</div></fieldset></form></section><section class="card live-selection"><div class="card-head"><h2>福利规则</h2></div>${rules()}</section>`;
 }
 function recipients(){
  const list=model.recipients;if(!list||list.loading)return '<p class="hint">正在读取收件人…</p>';if(list.error)return notice(list.error);
  return `<div class="table-wrap op-recipient-table"><table><thead><tr><th>角色 / 账号</th><th>投递状态</th><th>实际收件角色</th><th>邮件回执</th></tr></thead><tbody>${list.rows.map(r=>`<tr><td><strong>${escape(r.name)}</strong><small class="mono">${escape(r.characterId)} / ${escape(r.accountId)}</small></td><td>${textState[r.state]}${r.failure?`<small>${escape(r.failure.message)}</small>`:''}</td><td class="mono">${escape(r.deliveredCharacterId||'—')}</td><td><span class="mono">${escape(r.mailId||'—')}</span>${r.delivery?`<small>投递于 ${escape(stamp(r.delivery.grantedAt))}</small><small>${reward(r.delivery.reward)}</small>`:''}</td></tr>`).join('')}</tbody></table></div><div class="op-recipient-cards">${list.rows.map(r=>`<article><strong>${escape(r.name)}</strong><p class="hint mono">${escape(r.characterId)} / ${escape(r.accountId)}</p><dl class="detail-grid"><div><dt>投递状态</dt><dd>${textState[r.state]}</dd></div><div><dt>实际收件角色</dt><dd>${escape(r.deliveredCharacterId||'—')}</dd></div><div><dt>邮件回执</dt><dd>${escape(r.mailId||'—')}</dd></div></dl>${r.failure?`<p>${escape(r.failure.message)}</p>`:''}${r.delivery?`<p>${reward(r.delivery.reward)}</p><small>投递于 ${escape(stamp(r.delivery.grantedAt))}</small>`:''}</article>`).join('')}</div>${paging('recipients',list)}`;
 }
 function taskDetail(){
  const selected=model.selected;if(!selected)return '';if(selected.loading)return '<section class="card live-selection">正在读取任务…</section>';if(selected.error)return `<section class="card live-selection">${notice(selected.error)}${button('刷新任务','refresh-task')}</section>`;
  const j=selected.data;return `<section class="card live-selection" aria-label="任务详情"><div class="card-head"><h2>${escape(j.reward.title)}</h2><div class="actions">${model.run?.id===j.id?button(model.run.stop?'正在暂停…':'暂停发放','pause-run',j.id,model.run.stop):''}${button('刷新任务','refresh-task',j.id,model.busy)}${j.pending?button(j.state==='preview'?'确认发放':'继续发放','confirm-run',j.id,model.busy):''}${j.failed?button('重试失败项','confirm-retry',j.id,model.busy):''}</div></div><dl class="detail-grid"><div><dt>任务状态</dt><dd>${textState[j.state]}</dd></div><div><dt>发放范围</dt><dd>${recipientScope(j)}</dd></div><div><dt>收件人</dt><dd>${j.total} 个 · 待发 ${j.pending} · 成功 ${j.sent} · 失败 ${j.failed}</dd></div><div><dt>奖励</dt><dd>${reward(j.reward)}</dd></div>${j.recipientSource?`<div><dt>在线名单时间</dt><dd>${escape(stamp(j.recipientSource.observedAt))}</dd></div>`:''}</dl><p>${escape(j.reward.body)}</p><p class="hint mono">任务 ${escape(j.id)} · ${escape(stamp(j.createdAt))}</p>${recipients()}</section>`;
 }
 function tasks(){
  const list=model.tasks;
  let content=!list||list.loading?'<p class="hint">正在读取任务…</p>':list.error?notice(list.error):`<div class="table-wrap"><table><thead><tr><th>任务</th><th>范围</th><th>进度</th><th>状态</th><th>操作</th></tr></thead><tbody>${list.rows.map(j=>`<tr><td><strong>${escape(j.reward.title)}</strong><small>${escape(stamp(j.createdAt))}</small></td><td>${recipientScope(j)}</td><td>${j.sent} / ${j.total}${j.failed?` · ${j.failed} 失败`:''}</td><td>${textState[j.state]}</td><td>${button('查看回执','open-task',j.id)}</td></tr>`).join('')||'<tr><td colspan="5">暂无发放任务</td></tr>'}</tbody></table></div>${paging('tasks',list)}`;
  return `<div class="page-heading"><h1>任务记录</h1>${button('刷新列表','refresh-tasks')}</div><section class="card">${content}</section>${taskDetail()}`;
 }
 function view(){return pvf.banner()+(model.error?notice(model.error):'')+(model.message?`<p class="hint" role="status">${escape(model.message)}</p>`:'')+(state.route==='mail'?`${model.run?button(model.run.stop?'正在暂停…':'暂停发放','pause-run',model.run.id,model.run.stop):''}${model.selected&&!model.run?button('查看发放记录','open-task',model.selected.id):''}${mail()}`:tasks());}
 function confirmRun(retry){
  const j=model.selected?.data;if(!j)return;
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">${retry?'重试失败投递':'确认发放邮件'}</h2>${button('取消','close')}</div><h3>${escape(j.reward.title)}</h3><p>${reward(j.reward)}</p><p>${retry?`本次重试最多 10 个失败目标，共 ${j.failed} 个。`:`向全部 ${j.pending} 个待发角色投递，每批 10 人连续执行。可暂停后继续；${state.capabilities?.data.backgroundMail?'关闭页面后仍会继续。':'关闭页面后从任务记录续发。'}`}</p><div id="confirmation-recipients" aria-live="polite">正在读取固定名单…</div><div class="actions">${button('取消','close')}<button class="button primary" data-op="run" data-retry="${retry}">确认并发放</button></div>`;$('#dialog').showModal();confirmationRecipients(j);
 }
 function confirmationRecipients(job){
  const box=$('#confirmation-recipients'),confirm=$('#dialog [data-op="run"]');if(!box||!confirm)return;
  confirm.disabled=true;
  request('confirmation-recipients',`v1/tasks/${job.id}/confirmation`,body=>{
   if(!box.isConnected||model.selected?.id!==job.id)return;
   if(body.data.id!==job.id||body.data.revision!==job.revision||body.data.recipients.length!==job.total){box.innerHTML=notice({message:'任务已变化，请重新打开确认名单。'});return;}
   box.innerHTML=`<p>最终收件人 ${job.total} 个</p><div class="confirmation-roster">${body.data.recipients.map(r=>`<div>${escape(r.name)} · ${escape(r.accountId)} / ${escape(r.characterId)}</div>`).join('')}</div>`;
   confirm.disabled=false;
  },error=>{if(box.isConnected)box.innerHTML=notice(error);});
 }
 function watchScheduled(job){
  const run=model.run?.id===job.id&&model.run.external?model.run:{id:job.id,stop:false,external:true};model.run=run;
  const show=result=>{
   if(model.selected?.id===result.id)model.selected.data=result;
   const running=result.schedule==='active';
   model.message=`${result.schedule==='paused'&&result.pending?'已暂停 · ':''}已投递 ${result.sent} / ${result.total}，失败 ${result.failed}。${running?'正在继续…':result.schedule==='conflict'?'任务已变更，请重新确认。':''}`;
   if(!running){if(model.run===run)model.run=null;paint();return;}
   paint();setTimeout(()=>{if(model.run!==run||!active())return;request('scheduled-task',`v1/tasks/${job.id}`,body=>show(body.data),error=>{if(model.run===run)model.run=null;model.error=error;paint();});},1000);
  };show(job);
 }
 function runBatches(job,retry){
  if(model.busy)return;
  if(state.capabilities?.data.backgroundMail){write(`tasks/${job.id}/schedule`,{revision:job.revision,confirm:true,retry},null,watchScheduled);return;}
  const run={id:job.id,stop:false};model.run=run;
  const finish=result=>{
   if(model.run===run)model.run=null;
   model.message=`${run.stop&&result.pending?'已暂停 · ':''}已投递 ${result.sent} / ${result.total}，失败 ${result.failed}。`;
   if(active()&&state.route==='tasks'&&model.selected?.id===result.id){openTask(result.id);loadList('tasks');}
  };
  const next=current=>write(`tasks/${current.id}/run`,{revision:current.revision,confirm:true,retry},null,result=>{
   if(model.selected?.id===result.id)model.selected={id:result.id,data:result,loading:false,error:null};
   if(model.run===run&&!run.stop&&!retry&&active()&&result.pending>0&&result.pending<current.pending){
    model.message=`已投递 ${result.sent} / ${result.total}，正在继续…`;next(result);
   }else finish(result);
  },()=>{
   if(model.run===run)model.run=null;
   if(active()&&state.route==='tasks'&&model.selected?.id===job.id)openTask(job.id);
  });
  next(job);
 }
 function confirmRule(c){
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">${c.enabled?'停用':'启用'}福利规则</h2>${button('取消','close')}</div><h3>${escape(c.reward.title)}</h3><p>${scopeName(c.scope)} · ${reward(c.reward)}</p><p>${c.autoNewCharacters?'启用后自动发给现有及以后新建的角色。':'启用后自动发给现有角色。'} 已发对象不会重复发放。</p><div class="actions"><button class="button primary" data-op="apply-rule-state" data-id="${c.id}">确认${c.enabled?'停用':'启用'}</button></div>`;$('#dialog').showModal();
 }
 function selectCharacter(c){if(model.mail.recipientMode==='selected')addRecipient(c);else{model.mail.recipientMode='single';model.mail.accountId=c.accountId;model.mail.characterId=c.characterId;model.mail.key=newKey();}model.tab='mail';save();$('#dialog').close();location.hash='mail';}
 function selectRecipients(accounts,characters){model.mail={...model.mail,recipientMode:'selected',accounts:accounts.map(a=>({accountId:a.accountId,name:a.name})),recipients:characters.map(c=>({accountId:c.accountId,characterId:c.characterId,name:c.name})),accountId:'',characterId:'',key:newKey()};model.tab='mail';model.error=null;save();location.hash='mail';}
 function selectItem(i){const d=model.tab==='rule'?model.rule:model.mail;d.itemId=i.itemId;d.count=1;d.options={};d.pvfChecksum='';model.grant=null;d.key=newKey();save();location.hash='mail';}
 document.addEventListener('input',event=>{
  const form=event.target.closest('#op-compose');if(!form)return;const d=model[form.dataset.kind],el=event.target;
  if(el.name.startsWith('grant.')){const name=el.name.slice(6);d.options[name]=name==='equipmentQuality'?el.value:Number(el.value);}
  else d[el.name]=el.type==='checkbox'?el.checked:['count','gold'].includes(el.name)?Number(el.value):el.value;
  if(el.name==='itemId'){d.options={};d.pvfChecksum='';model.grant=null;const box=form.querySelector('#op-grant-fields');if(box)box.innerHTML=grantFields(d);}
  d.key=newKey();save();
 });
 document.addEventListener('change',event=>{if(event.target.closest('#op-compose')&&event.target.name==='itemId'&&model[model.tab].itemId)loadGrant();});
 document.addEventListener('submit',event=>{
  if(event.target.id!=='op-compose')return;event.preventDefault();const kind=event.target.dataset.kind,d=model[kind];
  const body={title:d.title,body:d.body,gold:d.gold,itemId:d.itemId,count:d.count};
  if(kind==='mail'){
   if(d.recipientMode==='selected'&&(d.accountId||d.characterId)){model.error={message:'请先将填写的角色加入名单'};paint();return;}
   if(d.itemId&&(!model.grant?.data||model.grant.itemId!==d.itemId||model.grant.kind!==kind)){model.error={message:'请先读取附件的发放参数'};paint();return;}
   if(d.recipientMode==='online'){write('mail/online-preview',{...body,options:d.options,pvfChecksum:d.itemId?d.pvfChecksum:''},d.key,previewed);return;}
   const recipients=d.recipientMode==='selected'?d.recipients.map(({accountId,characterId})=>({accountId,characterId})):[{accountId:d.accountId,characterId:d.characterId}];
   write('mail/preview',{...body,recipients,...(d.recipientMode==='selected'?{accountIds:d.accounts.map(a=>a.accountId)}:{}),options:d.options,pvfChecksum:d.itemId?d.pvfChecksum:''},d.key,previewed);
  }
  else {if(d.itemId&&(!model.grant?.data||model.grant.itemId!==d.itemId||model.grant.kind!==kind)){model.error={message:'请先读取附件的发放参数'};paint();return;}write('benefits',{...body,scope:d.scope,autoNewCharacters:d.autoNewCharacters,...(d.itemId?{options:d.options,pvfChecksum:d.pvfChecksum}:{})},d.key,c=>{model.message=`已保存“${c.reward.title}”，可在规则列表启用。`;loadList('benefits',true);});}
 });
 document.addEventListener('click',event=>{
  const el=event.target.closest('[data-op]');if(!el||el.disabled)return;const {op,id}=el.dataset;
  if(op==='close'){$('#dialog').close();return;}
  if(!active())return;
  if(op==='pause-run'){if(model.run?.external){const j=model.selected?.data;if(j)write(`tasks/${id}/pause`,{revision:j.revision},null,watchScheduled);}else if(model.run)model.run.stop=true;paint();return;}
  if(op==='all-characters-benefit'){model.rule={...fresh(),title:'金币福利',scope:'character',autoNewCharacters:true};model.tab='rule';model.grant=null;model.error=null;model.message='';save();paint();return;}
  if(op==='grant-load'){loadGrant();return;}
  if(['recipient-single','recipient-selected','recipient-online'].includes(op)){const mode=op.slice(10);if(model.mail.recipientMode!==mode){model.mail.recipientMode=mode;model.mail.key=newKey();save();}paint();return;}
  if(op==='new-online'){model.mail.key=newKey();model.message='下次预览将重新获取在线名单';save();paint();return;}
  if(op==='recipient-add'){addRecipient({accountId:model.mail.accountId,characterId:model.mail.characterId});paint();return;}
  if(op==='recipient-remove'){model.mail.recipients=model.mail.recipients.filter(r=>r.characterId!==id);model.mail.key=newKey();save();paint();return;}
  if(op==='account-remove'){model.mail.accounts=model.mail.accounts.filter(a=>a.accountId!==id);model.mail.key=newKey();save();paint();return;}
  if(op==='tab-mail'||op==='tab-rule'){model.tab=op==='tab-rule'?'rule':'mail';model.grant=null;paint();if(model[model.tab].itemId)loadGrant();}
  else if(op==='new-mail'){model.mail=fresh();model.tab='mail';model.error=null;model.message='';save();paint();}
  else if(op==='refresh-benefits')loadList('benefits');
  else if(op==='rule-pvf')pvf.open(id);
  else if(op==='rule-pvf-history')pvf.open(id,true);
  else if(op==='refresh-tasks')loadList('tasks');
  else if(op==='open-task'||op==='refresh-task'){openTask(id||model.selected.id);if(state.route!=='tasks')location.hash='tasks';}
  else if(op==='confirm-run'||op==='confirm-retry')confirmRun(op==='confirm-retry');
  else if(op==='run'){
   const j=model.selected.data;$('#dialog').close();runBatches(j,el.dataset.retry==='true');
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
 return {view,load,leave,reset,selectCharacter,selectRecipients,selectItem};
}
