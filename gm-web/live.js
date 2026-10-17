import {$, escape, icon} from './ui.js';
import {nav,setNavigation} from './shell.js';
import {createOperations} from './operations.js';
import {createAnnouncements} from './announcements.js';
import {renderItemDetail} from './item-detail-view.js';
import {createCurrency} from './currency.js';
import {createCharacterResources} from './character-resources.js';

const state = {route:'overview', auth:'checking', capabilities:null, authError:'', busy:false,
  draft:{q:'',scope:'accounts',status:'',kind:''}, list:null, selected:null, detail:null, itemDetail:null};
const requests = new Map();
const badge = (text,tone='') => `<span class="badge ${tone}">${escape(text)}</span>`;
const action = (label,name,disabled=false) => `<button class="button" data-action="${name}" ${disabled?'disabled':''}>${label}</button>`;
const stamp = value => value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString('zh-CN',{hour12:false}) : '暂无记录';
const heading = (title,description,actions='') => `<div class="page-heading"><h1>${title}</h1><div class="actions">${actions}</div></div>`;
const empty = (title,description) => `<div class="empty-state"><strong>${escape(title)}</strong><p>${escape(description)}</p></div>`;
const errorHTML = error => `<div class="hint-box" role="alert"><strong>${escape(error.message)}</strong>${error.requestId?`<p class="mono">请求编号：${escape(error.requestId)}</p>`:''}</div>`;

function cancel(slot) {requests.get(slot)?.abort(); requests.delete(slot);}
function cancelData() {currency.reset();resources.reset();for (const slot of ['list','selection','character','item-detail']) cancel(slot); $('#dialog').close(); state.selected=null;state.detail=null;state.itemDetail=null;}
function forgetSession(message='') {
  currency.reset();
  operations.reset();
  announcements.reset();
  for(const slot of [...requests.keys()])cancel(slot);
  state.auth='required';state.capabilities=null;state.list=null;state.selected=null;state.detail=null;state.itemDetail=null;state.authError=message;state.busy=false;
  $('#dialog').close();render();
}
async function request(slot,path,success,failure,options={}) {
  cancel(slot);const controller=new AbortController();requests.set(slot,controller);
  // A per-request deadline also covers a stalled proxy. No automatic retry/poll.
  const timeout=setTimeout(()=>controller.abort('timeout'),10000);
  try {
    const endpoint=path.startsWith('/admin/api/')?path:`/api/${path}`;
    const response=await fetch(endpoint,{credentials:'same-origin',cache:'no-store',...options,signal:controller.signal});
    if(requests.get(slot)!==controller)return;
    if(response.status===401){forgetSession('登录已过期，请重新登录。');return;}
    let body;
    try{body=await response.json();}catch{throw new Error('服务返回了无法识别的响应，请重试。');}
    if(!response.ok||body.success===false||body.error){
      const err=new Error(typeof body.error==='string'?body.error:body.error?.message||`请求失败（${response.status}）`);
      err.requestId=body.requestId;err.status=response.status;throw err;
    }
    if(requests.get(slot)===controller)success(body);
  } catch(error) {
    if(requests.get(slot)!==controller)return;
    if(controller.signal.reason==='timeout')error=new Error('请求超时，请重试。');
    else if(error.name==='AbortError')return;
    failure(error);
  } finally {clearTimeout(timeout);if(requests.get(slot)===controller)requests.delete(slot);}
}
function connect() {
  state.auth='checking';state.authError='';render();
  request('auth','v1/capabilities',body=>{
    state.capabilities=body;state.auth='ready';render();loadRoute();
  },error=>{state.auth='error';state.authError=error.message;render();});
}
function newList() {
  const resource=state.route==='items'?'items':state.draft.scope;
  return {resource,params:{...state.draft},cursors:[''],index:0,next:'',rows:[],total:null,loading:false,error:null,observedAt:''};
}
function loadRoute() {
  if(state.auth!=='ready')return;
  if(['players','items'].includes(state.route)){state.list=newList();loadList();}
  if(['mail','tasks'].includes(state.route))operations.load();
  if(state.route==='gateway')announcements.load();
}
function loadList() {
  cancelData();const list=state.list;if(!list)return;
  list.loading=true;list.error=null;list.rows=[];list.next='';list.observedAt='';render();
  const query=new URLSearchParams({limit:'20',q:list.params.q});
  if(list.cursors[list.index])query.set('cursor',list.cursors[list.index]);
  if(list.resource==='accounts'&&list.params.status)query.set('state',list.params.status);
  if(list.resource==='items'&&list.params.kind)query.set('kind',list.params.kind);
  request('list',`v1/${list.resource}?${query}`,body=>{
    if(state.list!==list)return;
    list.rows=list.resource==='items'?body.data.items:body.data;
    list.total=list.resource==='items'?body.data.total:null;
    list.next=body.page.nextCursor||'';list.observedAt=body.observedAt;list.loading=false;render();
  },error=>{if(state.list!==list)return;list.loading=false;list.error=error;render();});
}
function selectAccount(id) {
  currency.reset();
  cancel('character');$('#dialog').close();state.detail=null;
  const selected={id,tab:'characters',loading:true,error:null,characters:[]};state.selected=selected;render();
  request('selection',`v1/accounts/${encodeURIComponent(id)}/characters`,body=>{
    if(state.selected!==selected)return;selected.characters=body.data;selected.loading=false;selected.observedAt=body.observedAt;render();
  },error=>{if(state.selected!==selected)return;selected.loading=false;selected.error=error;render();});
}
function showCharacter(id) {
  resources.reset();
  const detail={id,loading:true,error:null,data:null};state.detail=detail;renderDialog();$('#dialog').showModal();
  request('character',`v1/characters/${encodeURIComponent(id)}`,body=>{
    if(state.detail!==detail)return;detail.data=body.data;detail.observedAt=body.observedAt;detail.loading=false;renderDialog();
  },error=>{if(state.detail!==detail)return;detail.error=error;detail.loading=false;renderDialog();});
}
function showItem(id) {
 const detail={id,loading:true,error:null,data:null};state.itemDetail=detail;render();
 request('item-detail',`v1/items/${encodeURIComponent(id)}`,body=>{if(state.itemDetail!==detail)return;detail.data=body.data;detail.loading=false;render();},error=>{if(state.itemDetail!==detail)return;detail.error=error;detail.loading=false;render();});
}
function itemSelection(){
 const d=state.itemDetail;
 if(!d)return empty('选择一件物品','查看属性、效果与套装。');
 if(d.loading)return empty('正在读取物品详情','请稍候…');
 if(d.error)return errorHTML(d.error)+action('重试','retry-item');
 return renderItemDetail(d.data)+`<div class="actions"><button type="button" class="button primary" data-mail-item="${escape(d.id)}">用于邮件</button></div>`;
}
function characterTable(rows) {
  return `<div class="table-wrap"><table><thead><tr><th>角色</th><th>等级</th><th>职业编号</th><th>所属账号</th><th>操作</th></tr></thead><tbody>${rows.map(c=>`<tr><td><strong>${escape(c.name)}</strong><small class="mono">${escape(c.characterId)}</small></td><td>${escape(c.level)}</td><td>${escape(c.job)}</td><td class="mono">${escape(c.accountId)}</td><td><button class="button link" data-character="${escape(c.characterId)}" aria-label="查看${escape(c.name)}">查看</button></td></tr>`).join('')}</tbody></table></div>`;
}
function renderDialog() {
  const detail=state.detail;if(!detail)return;
  let content=empty('正在读取角色','请稍候…');
  if(detail.error)content=errorHTML(detail.error);
  else if(detail.data){const c=detail.data.character;content=`<div class="detail-identity"><span class="avatar">${escape(c.name.slice(0,1))}</span><div><h2>${escape(c.name)}</h2><p>Lv.${escape(c.level)} · 职业编号 ${escape(c.job)}</p></div></div><dl class="detail-grid"><div><dt>角色 ID</dt><dd class="mono">${escape(c.characterId)}</dd></div><div><dt>所属账号</dt><dd class="mono">${escape(c.accountId)}</dd></div><div><dt>在线状态</dt><dd>未知</dd></div><div><dt>创建时间</dt><dd>${escape(stamp(c.createdAt))}</dd></div></dl><p class="hint">读取于 ${escape(stamp(detail.observedAt))}。</p>`;}
  $('#dialog').classList.add('character-dialog');
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">角色资料${detail.data?' · '+escape(detail.data.character.name):''}</h2>${action('关闭','close-dialog')}</div>${detail.data?resources.tabs():''}${detail.data&&!resources.isProfile()?resources.view():content}${detail.data&&resources.isProfile()?`<div class="actions">${action('发送邮件','mail-character')}</div>`:''}`;
}
function selection() {
  const s=state.selected;
  if(!s)return empty('选择一个账号','打开账号后查看它的角色。');
  const account=state.list?.rows.find(a=>a.accountId===s.id);
  const tabs=`<div class="actions account-tabs">${action('角色列表','account-characters')}${action('点券与流水','account-currency')}</div>`;
  if(s.tab==='currency')return `<div class="card-head"><h2>${escape(account?.name||s.id)}</h2><span class="mono">${escape(s.id)}</span></div>${tabs}${currency.view()}`;
  return `<div class="card-head"><div><h2>${escape(account?.name||s.id)}</h2><p class="mono">${escape(s.id)}</p></div>${action('刷新角色','refresh-selection',s.loading)}</div>${tabs}${s.loading?empty('正在读取角色','请稍候…'):s.error?errorHTML(s.error):s.characters.length?characterTable(s.characters):empty('此账号没有角色','当前未找到有效角色。')}${s.observedAt?`<p class="hint">读取于 ${escape(stamp(s.observedAt))}</p>`:''}`;
}
function searchForm() {
  const d=state.draft,items=state.route==='items';
  return `<form id="directory-search" class="live-search"><label>查询${items?'物品':'账号或角色'}<input name="q" aria-label="${items?'查询物品':'查询账号或角色'}" value="${escape(d.q)}" maxlength="128" placeholder="${items?'物品名称或 ID':'输入名称或 ID'}"></label>${items?`<label>物品类型<select name="kind">${[['','全部类型'],['equipment','装备'],['stackable','道具']].map(([v,t])=>`<option value="${v}" ${d.kind===v?'selected':''}>${t}</option>`).join('')}</select></label>`:`<label>查询范围<select name="scope"><option value="accounts" ${d.scope==='accounts'?'selected':''}>账号</option><option value="characters" ${d.scope==='characters'?'selected':''}>角色</option></select></label><label>账号状态<select name="status" ${d.scope==='characters'?'disabled':''}>${[['','全部状态'],['enabled','正常'],['disabled','已停用']].map(([v,t])=>`<option value="${v}" ${d.status===v?'selected':''}>${t}</option>`).join('')}</select></label>`}<button class="button primary" type="submit">查询</button>${action('重置','reset')}</form>`;
}
function directory() {
  const items=state.route==='items',list=state.list;
  let content=empty('准备查询','请稍候…');
  if(list){
    if(list.loading)content=empty('正在查询','只读取当前页，请稍候…');
    else if(list.error)content=errorHTML(list.error)+`<div class="actions">${action('重试','refresh')}</div>`;
    else if(!list.rows.length)content=empty('没有匹配结果','请调整条件后重新查询。');
    else if(list.resource==='accounts')content=`<div class="table-wrap"><table><thead><tr><th>账号</th><th>状态</th><th>角色数</th><th>最近登录</th><th>操作</th></tr></thead><tbody>${list.rows.map(a=>`<tr ${state.selected?.id===a.accountId?'class="selected"':''}><td><strong>${escape(a.name)}</strong><small class="mono">${escape(a.accountId)}</small></td><td>${badge(a.disabled?'已停用':'正常',a.disabled?'warn':'good')}</td><td>${escape(a.characterCount)}</td><td>${escape(stamp(a.lastLoginAt))}</td><td><button class="button link" data-account="${escape(a.accountId)}" aria-label="打开${escape(a.name)}">打开</button></td></tr>`).join('')}</tbody></table></div>`;
    else if(list.resource==='characters')content=characterTable(list.rows);
    else content=`<div class="table-wrap"><table><thead><tr><th>物品</th><th>类型</th><th>等级</th><th>操作</th></tr></thead><tbody>${list.rows.map(i=>`<tr ${state.itemDetail?.id===i.itemId?'class="selected"':''}><td><strong>${escape(i.name)}</strong><small class="mono">${escape(i.itemId)}</small>${i.invalid?badge('需核查','warn'):''}</td><td>${i.kind==='equipment'?'装备':'道具'}</td><td>${escape(i.minLevel)}</td><td><button type="button" class="button link" data-item-detail="${escape(i.itemId)}" aria-label="查看${escape(i.name)}详情">详情</button><button type="button" class="button link" data-mail-item="${escape(i.itemId)}" ${i.invalid?'disabled':''}>用于邮件</button></td></tr>`).join('')}</tbody></table></div>`;
  }
  const footer=list?`<div class="table-footer"><span>第 ${list.index+1} 页 · 本页 ${list.rows.length} 条${list.total!==null?` · 共 ${escape(list.total)} 项`:''}</span><div class="actions">${action('上一页','prev',list.loading||list.index===0)}${action('下一页','next',list.loading||!list.next)}</div></div>${list.observedAt?`<p class="hint">读取于 ${escape(stamp(list.observedAt))}</p>`:''}`:'';
  return heading(items?'物品目录':'账号与角色',items?'按名称或 ID 查询物品目录。':'选择查询范围，提交条件后读取当前页。',action('刷新','refresh',list?.loading))+`${items?'<div class="split-view live-catalog">':''}<section class="card">${searchForm()}<div aria-live="polite" aria-busy="${!!list?.loading}">${content}${footer}</div></section>${items?`<section class="card" aria-label="物品详情" aria-live="polite">${itemSelection()}</section></div>`:''}${!items&&list?.resource==='accounts'?`<section class="card live-selection" aria-label="账号详情">${selection()}</section>`:''}`;
}
function overview() {
  const labels={'currency.read':'账号点券与流水','currency.write':'点券增减','accounts.read':'账号查询','characters.read':'角色查询','inventory.read':'背包、穿戴与仓库','mailbox.read':'角色邮箱','quests.read':'游戏任务与任务库','items.read':'物品目录','mail.write':'邮件发放','benefits.write':'福利规则','tasks.read':'运营作业与回执'};
  return heading('工作台概览','按需查询运营数据，保留每次读取时间。',action('刷新连接状态','reconnect'))+`<section class="card"><div class="card-head"><h2>当前服务能力</h2>${badge('查询服务','blue')}</div>${Object.entries(labels).map(([key,label])=>`<div class="service-row"><span class="service-icon">${icon('server')}</span><div><strong>${label}</strong><p>${state.capabilities?.data.features[key]?'可用':'服务待就绪'}</p></div>${badge(state.capabilities?.data.features[key]?'可查询':'待就绪',state.capabilities?.data.features[key]?'good':'warn')}</div>`).join('')}<p class="hint">读取于 ${escape(stamp(state.capabilities?.observedAt))}。</p></section><section class="card live-selection"><h2>常用入口</h2><div class="actions live-shortcuts"><a class="button primary" href="#players">账号与角色</a><a class="button" href="#items">物品目录</a><a class="button" href="#mail">邮件与福利</a><a class="button" href="preview.html">视觉样版</a></div><p class="hint">邮件、福利规则和投递记录由此进入。</p></section>`;
}
function settings() {
  return heading('工作台设置','外观偏好保存在当前浏览器。')+`<section class="card"><div class="settings-row"><span class="service-icon">${icon('palette')}</span><div><h2>主题与外观</h2><p>配色、明暗模式、字体、圆角、密度和效果偏好。</p></div>${action('主题工作室','theme')}</div><div class="settings-row"><span class="service-icon">${icon('clock')}</span><div><h2>按需查询</h2><p>提交查询或点击刷新时更新数据。</p></div>${badge('主动刷新','blue')}</div></section>`;
}
function login() {
  if(state.auth==='checking')return heading('连接管理工作台','正在检查登录状态…');
  if(state.auth==='error')return heading('暂时无法连接','请检查管理服务后重试。')+`<section class="card">${errorHTML({message:state.authError})}${action('重试连接','reconnect')}</section>`;
  return heading('登录管理工作台','请输入 GM 管理密码。')+`<section class="card live-login"><form id="login-form" class="field-stack"><label>管理密码<input type="password" name="password" required autocomplete="current-password" maxlength="1024"></label><label>导入登录 Key<input id="login-key-file" type="file" accept=".json,.key,.txt"></label>${state.authError?errorHTML({message:state.authError}):''}<button class="button primary" ${state.busy?'disabled':''}>${state.busy?'正在登录…':'登录'}</button></form></section>`;
}
function render() {
  $('#main').innerHTML=state.auth!=='ready'?login():state.route==='overview'?overview():['players','items'].includes(state.route)?directory():state.route==='settings'?settings():['mail','tasks'].includes(state.route)?operations.view():announcements.view();
  $('#breadcrumb-current').textContent=nav.find(([r])=>r===state.route)[1];
  $('#session-control').hidden=state.auth!=='ready';
  document.title=`${$('#breadcrumb-current').textContent} · PurpleMagic GM`;
  document.querySelectorAll('#navigation a').forEach(link=>{if(link.hash===`#${state.route}`)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');});
}
function navigate() {
  operations.leave();
  announcements.leave();
  cancelData();const route=location.hash.slice(1);state.route=nav.some(([r])=>r===route)?route:'overview';
  state.draft={q:'',scope:'accounts',status:'',kind:''};state.list=null;setNavigation(false);render();loadRoute();
}
$('.topbar-actions').insertAdjacentHTML('beforeend','<button class="button subtle" id="session-control" data-action="logout" hidden>退出</button>');
window.addEventListener('hashchange',()=>{navigate();$('#main').focus({preventScroll:true});});
$('#dialog').addEventListener('close',()=>{if(!$('#dialog').open){resources.reset();cancel('character');state.detail=null;$('#dialog').classList.remove('character-dialog');$('#dialog').replaceChildren();}});
document.addEventListener('input',event=>{if(event.target.closest('#directory-search'))state.draft[event.target.name]=event.target.value;});
document.addEventListener('change',async event=>{
  if(event.target.id==='login-key-file'){
    const file=event.target.files?.[0];if(!file)return;
    try{if(file.size>4096)throw Error('登录 Key 文件过大。');const text=(await file.text()).trim();const key=text.startsWith('{')?JSON.parse(text).key:text;if(typeof key!=='string'||key.length<32||key.length>1024)throw Error('登录 Key 格式无效。');const input=document.querySelector('#login-form input[name=password]');if(input)input.value=key;}catch(error){state.authError=error.message;render();}
    return;
  }
  if(!event.target.closest('#directory-search'))return;state.draft[event.target.name]=event.target.value;
  if(event.target.name==='scope')$('#directory-search select[name=status]').disabled=event.target.value==='characters';
});
document.addEventListener('submit',event=>{
  event.preventDefault();const form=event.target;
  if(form.id==='directory-search'){state.list=newList();loadList();}
  if(form.id==='login-form'){
    const password=new FormData(form).get('password');form.reset();state.busy=true;state.authError='';render();
    request('auth','auth/login',()=>{state.busy=false;connect();},error=>{state.busy=false;state.authError=error.message;render();},{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password})});
  }
});
document.addEventListener('click',event=>{
  const control=event.target.closest('button');if(!control)return;
  if(control.dataset.account){selectAccount(control.dataset.account);return;}
  if(control.dataset.character){showCharacter(control.dataset.character);return;}
  if(control.dataset.itemDetail){showItem(control.dataset.itemDetail);return;}
  if(control.dataset.mailItem){const item=state.list?.rows.find(i=>i.itemId===control.dataset.mailItem);if(item)operations.selectItem(item);return;}
  switch(control.dataset.action){
    case 'account-currency':if(state.selected){state.selected.tab='currency';currency.open(state.selected.id);render();}break;
    case 'account-characters':if(state.selected){currency.reset();state.selected.tab='characters';render();}break;
    case 'theme':window.__SOFT_THEME_STUDIO__.open(control);break;
    case 'close-dialog':$('#dialog').close();break;
    case 'mail-character':if(state.detail?.data?.character)operations.selectCharacter(state.detail.data.character);break;
    case 'reconnect':cancelData();state.list=null;connect();break;
    case 'refresh':loadList();break;
    case 'retry-item':if(state.itemDetail)showItem(state.itemDetail.id);break;
    case 'refresh-selection':if(state.selected)selectAccount(state.selected.id);break;
    case 'reset':state.draft={q:'',scope:'accounts',status:'',kind:''};state.list=newList();loadList();break;
    case 'prev':if(state.list?.index>0){state.list.index--;loadList();}break;
    case 'next':if(state.list?.next){const l=state.list;l.cursors=l.cursors.slice(0,l.index+1);l.cursors.push(l.next);l.index++;loadList();}break;
    case 'logout':
      forgetSession();state.auth='checking';render();
      request('auth','auth/logout',()=>forgetSession(),error=>{state.auth='error';state.authError=`退出尚未确认：${error.message}`;render();},{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});break;
  }
});
const currency=createCurrency({request,cancel,render,state});
const operations=createOperations({request,cancel,render,state});
const announcements=createAnnouncements({request,cancel,render,state});
const resources=createCharacterResources({request,cancel,state,renderDialog});
navigate();connect();
