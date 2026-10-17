import {$,escape} from './ui.js';

const key=()=>Array.from(crypto.getRandomValues(new Uint8Array(16)),n=>n.toString(16).padStart(2,'0')).join('');
const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
const button=(label,action,disabled=false)=>`<button type="button" class="button" data-pvf="${action}" ${disabled?'disabled':''}>${label}</button>`;
const storage='soft-gm-benefit-pvf-v1';
const labels={itemName:'物品名称',pvfChecksum:'PVF 摘要','definition.scriptSha256':'脚本摘要','definition.path':'脚本路径','definition.kind':'类型','definition.tag':'分类','definition.rarity':'稀有度','definition.minLevel':'使用等级','definition.grade':'物品等级','definition.stackLimit':'堆叠上限','definition.usablePeriodDays':'物品默认天数','definition.absoluteExpireUnix':'物品固定到期时间'};
const fields=p=>{
 const a=p.reward?.attachments?.[0],d=p.definition;
 return {itemName:p.itemName,pvfChecksum:p.pvfChecksum,...Object.fromEntries(Object.entries(d||{}).map(([k,v])=>['definition.'+k,v])),...(a?{'attachment.itemId':a.itemId,'attachment.count':a.count,...Object.fromEntries(Object.entries(a.options||{}).map(([k,v])=>['option.'+k,v]))}:{})};
};
Object.assign(labels,{'attachment.itemId':'物品编号','attachment.count':'数量','option.durability':'耐久度','option.maxDurability':'最大耐久','option.reinforce':'强化/增幅等级','option.amplifyType':'增幅类型','option.amplifyValue':'增幅数值','option.forgingLevel':'锻造等级','option.qualitySeed':'品级种子'});

function comparison(before,after){
 const a=fields(before),b=fields(after),names=[...new Set([...Object.keys(a),...Object.keys(b)])].filter(k=>!['definition.name'].includes(k));
 const primary=names.filter(k=>k==='itemName'||k.startsWith('attachment.')||k.startsWith('option.'));
 const rows=keys=>keys.map(k=>`<div class="pvf-field ${String(a[k])!==String(b[k])?'pvf-changed':''}"><strong>${escape(labels[k]||k)}</strong><div><span>原版本</span><code>${escape(a[k]??'未记录')}</code></div><div><span>当前版本</span><code>${escape(b[k]??'未记录')}</code></div></div>`).join('');
 return `<p class="hint mono">PVF ${escape(before.pvfChecksum?.slice(0,12)||'未记录')} → ${escape(after.pvfChecksum?.slice(0,12)||'未记录')}</p><div class="pvf-comparison">${rows(primary)}</div><details class="pvf-evidence"><summary>查看完整版本信息与脚本摘要</summary><div class="pvf-comparison">${rows(names.filter(k=>!primary.includes(k)))}</div></details>`;
}

export function createBenefitPVF({request,cancel,state,reload}){
 let pending;try{pending=JSON.parse(sessionStorage.getItem(storage));}catch{}
 let screen=null;
 const persist=()=>{try{if(pending)sessionStorage.setItem(storage,JSON.stringify(pending));else sessionStorage.removeItem(storage);}catch{}};
 const visible=s=>screen===s&&$('#dialog').open&&!!$('#dialog [data-pvf-view]');
 function draw(){
  const s=screen;if(!s)return;
  let body=s.loading?'<p class="hint">正在读取…</p>':'';
  if(s.error)body+=`<div class="hint-box" role="alert">${escape(s.error.message)}</div>`;
  if(s.preview){
   const p=s.preview;
   body+=`<h3>${escape(p.title)}</h3><p>${p.scope==='account'?'每账号一次':'每角色一次'}</p><p>${p.canApply?'确认后，未投递对象使用当前物品定义；已发回执和实际附件保持。':'此规则与当前 PVF 版本一致。'}</p>${!p.before.definition?'<p class="hint">历史单项脚本摘要未记录。以下列出已保存的物品参数和当前定义。</p>':''}${comparison(p.before,p.after)}<div class="actions">${p.canApply?button(s.busy?'正在确认…':s.pending?'重试确认':'确认使用当前 PVF','apply',s.busy):''}${s.pending&&['plan_conflict','pvf_conflict','revision_conflict','invalid_options','invalid_item','idempotency_conflict'].includes(s.error?.code)?button('重新核对','renew',s.busy):''}</div>`;
  }
  if(s.receipt)body+=`<p role="status">已核对，规则版本 ${escape(s.receipt.previousRevision)} → ${escape(s.receipt.revision)}</p><p>${escape(stamp(s.receipt.createdAt))} · ${escape(s.receipt.actor)}</p>${comparison(s.receipt.before,s.receipt.after)}`;
  if(s.history){body+=s.history.length?s.history.map(r=>`<details><summary>${escape(stamp(r.createdAt))} · 版本 ${r.previousRevision} → ${r.revision}</summary><p>${escape(r.actor)}</p>${comparison(r.before,r.after)}</details>`).join(''):'<p class="hint">暂无 PVF 核对记录</p>';body+=`<div class="actions">${button('上一页','prev',s.loading||s.index===0)}${button('下一页','next',s.loading||!s.next)}</div>`;}
  $('#dialog').innerHTML=`<div data-pvf-view><div class="dialog-head"><h2 id="dialog-title">${s.kind==='history'?'PVF 核对历史':'核对福利 PVF'}</h2>${button('关闭','close')}</div>${body}</div>`;
 }
 function open(id,history=false){
  cancel('benefit-pvf');screen={id,kind:history?'history':'preview',loading:true,index:0,cursors:[''],next:'',requestKey:key()};
  draw();if(!$('#dialog').open)$('#dialog').showModal();
  if(history){loadHistory();return;}
  const s=screen;
  post(`benefits/${id}/pvf-preview`,{},s.requestKey,body=>{if(!visible(s))return;s.loading=false;s.preview=body.data;draw();},s);
 }
 function post(path,body,id,success,s){
  request('benefit-pvf',`v1/${path}`,success,error=>{if(!visible(s))return;s.loading=false;s.busy=false;s.error=error;draw();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':id},body:JSON.stringify(body)});
 }
 function loadHistory(){
  const s=screen;s.loading=true;s.error=null;draw();const q=new URLSearchParams({limit:'10'});if(s.cursors[s.index])q.set('cursor',s.cursors[s.index]);
  request('benefit-pvf',`v1/benefits/${s.id}/pvf-history?${q}`,body=>{if(!visible(s))return;s.loading=false;s.history=body.data;s.next=body.page.nextCursor||'';draw();},error=>{if(!visible(s))return;s.loading=false;s.error=error;draw();});
 }
 function resume(){if(!pending)return;screen={id:pending.preview.id,kind:'preview',preview:pending.preview,requestKey:pending.key,pending:true};draw();if(!$('#dialog').open)$('#dialog').showModal();}
 function banner(){return pending?`<div class="hint-box">福利“${escape(pending.preview.title)}”有一笔待核实的 PVF 确认。${button('恢复确认','resume')}</div>`:'';}
 function leave(){cancel('benefit-pvf');if($('#dialog [data-pvf-view]'))$('#dialog').close();screen=null;}
 document.addEventListener('click',event=>{
  const b=event.target.closest('[data-pvf]');if(!b||b.disabled||state.auth!=='ready')return;
  const action=b.dataset.pvf,s=screen;
  if(action==='resume'){resume();return;}
  if(!s)return;
  if(action==='close'){leave();return;}
  if(action==='prev'||action==='next'){if(action==='prev')s.index--;else{s.cursors=s.cursors.slice(0,s.index+1);s.cursors.push(s.next);s.index++;}loadHistory();return;}
  if(action==='renew'){pending=null;persist();open(s.id);reload();return;}
  if(action==='apply'&&s.preview?.canApply&&!s.busy){
   if(pending&&pending.key!==s.requestKey){s.error={message:'请先恢复上一笔 PVF 确认'};draw();return;}
   pending={key:s.requestKey,preview:s.preview};persist();s.pending=true;s.busy=true;s.error=null;draw();
   post(`benefits/${s.id}/pvf-apply`,{...s.preview.command,confirm:true},s.requestKey,body=>{if(pending?.key===s.requestKey){pending=null;persist();}if(visible(s)){s.busy=false;s.preview=null;s.receipt=body.data;draw();}reload();},s);
  }
 });
 return {open,banner,leave};
}
