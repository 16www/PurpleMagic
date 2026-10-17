import {escape} from './ui.js';
const labels={building:'正在准备名单',ready:'待确认',running:'处理中',completed:'已完成',cancelled:'已结束',conflict:'邮箱已变更'};
const terminal=j=>['completed','cancelled','conflict'].includes(j?.status);
const button=(text,action,disabled=false)=>`<button type="button" class="button" data-mail-cleanup="${action}" ${disabled?'disabled':''}>${text}</button>`;

export function createMailboxCleanup({request,cancel,state,render,refresh,changesView}){
 const storageKey='soft-gm-mailbox-cleanup-v1';let active={};
 try{const v=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(v&&typeof v==='object'&&!Array.isArray(v))active=v;}catch{}
 const model=()=>state.detail?.resources?state.detail.resources.mailCleanup??={job:null,token:'',busy:false,auto:false,error:null,items:[],itemsNext:'',itemsCursors:[''],itemsPage:0,jobs:null,jobsNext:'',jobsCursors:[''],jobsPage:0}:null;
 const character=()=>state.detail?.data?.character;
 const scope=c=>JSON.stringify([c.accountId,c.characterId]);
 const path=()=>`v1/characters/${encodeURIComponent(character().characterId)}/mailbox-cleanups`;
 function remember(value){const c=character();if(!c)return;if(value)active[scope(c)]=value;else delete active[scope(c)];sessionStorage.setItem(storageKey,JSON.stringify(active));}
 function reset(){for(const key of ['cleanup-create','cleanup-advance','cleanup-read','cleanup-items','cleanup-jobs'])cancel(key);const m=model();if(m){m.busy=false;m.auto=false;}}
 function call(key,url,body,success,headers={}){
  const owner=state.detail,m=model();if(!m)return;
  m.busy=true;m.error=null;render();
  request(key,url,res=>{if(state.detail!==owner)return;m.busy=false;success(res);},e=>{if(state.detail!==owner)return;m.busy=false;m.auto=false;m.error=e;render();},body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,...headers},body:JSON.stringify(body)});
 }
 function receive(data,then){const m=model();m.job=data.job;m.token=data.planToken||'';try{remember({id:m.job.id});}catch{m.auto=false;m.error=Error('无法保存本页恢复位置，可从清理记录重新打开任务。');}if(terminal(m.job)){m.auto=false;refresh();}render();if(then)then();}
 function create(reason,discardUnclaimed){const m=model(),c=character();if(!m||!c||m.busy)return;
  if(!reason.trim()||new TextEncoder().encode(reason).length>500){m.error=Error('请填写清理原因，最多 500 字节。');render();return;}
  const pending={id:crypto.randomUUID(),body:{accountId:c.accountId,filter:state.detail.resources.mailFilter||{},discardUnclaimed,reason}};
  try{remember(pending);}catch{m.error=Error('浏览器无法保存请求编号，请启用会话存储后重试。');render();return;}
  m.items=[];m.job=null;m.auto=true;sendCreate(pending);
 }
 function sendCreate(pending){call('cleanup-create',path(),pending.body,res=>receive(res.data,()=>{const m=model();if(m.auto&&m.job.status==='building')advance('prepare');else{m.auto=false;readItems(true);}}),{'Idempotency-Key':pending.id});}
 function advance(mode){const m=model();if(!m?.job||m.busy)return;const j=m.job;
  const body={revision:j.revision};if(mode==='run'&&j.status==='ready'){body.confirm=true;body.planToken=m.token;}
  call('cleanup-advance',path()+'/'+encodeURIComponent(j.id)+'/'+mode,body,res=>receive(res.data,()=>{
   const now=model();if(mode==='cancel'&&!terminal(now.job)){advance('cancel');return;}
   if(now.auto&&((mode==='prepare'&&now.job.status==='building')||(mode==='run'&&now.job.status==='running'))){advance(mode);return;}
   now.auto=false;if(mode==='prepare'||!now.items.length)readItems(true);else render();
  }));
 }
 function open(id){const m=model();if(!m||m.busy)return;m.auto=false;m.items=[];call('cleanup-read',path()+'/'+encodeURIComponent(id),undefined,res=>receive(res.data,()=>readItems(true)));}
 function recover(){const c=character(),m=model(),p=c&&active[scope(c)];if(!p||m.busy)return;if(p.body){m.auto=true;sendCreate(p);}else open(p.id);}
 function readItems(resetPage=false){const m=model();if(!m?.job||m.busy)return;if(resetPage){m.itemsPage=0;m.itemsCursors=[''];}const q=new URLSearchParams({limit:'20'});if(m.itemsCursors[m.itemsPage])q.set('cursor',m.itemsCursors[m.itemsPage]);
  call('cleanup-items',path()+'/'+encodeURIComponent(m.job.id)+'/items?'+q,undefined,res=>{m.items=res.data;m.itemsNext=res.page?.nextCursor||'';render();});
 }
 function readJobs(resetPage=false){const m=model();if(!m||m.busy)return;if(resetPage){m.jobsPage=0;m.jobsCursors=[''];}const q=new URLSearchParams({limit:'20'});if(m.jobsCursors[m.jobsPage])q.set('cursor',m.jobsCursors[m.jobsPage]);
  call('cleanup-jobs',path()+'?'+q,undefined,res=>{m.jobs=res.data;m.jobsNext=res.page?.nextCursor||'';render();});
 }
 function view(){const m=model(),c=character();if(!m||!c)return '';const j=m.job,p=active[scope(c)];
  const filter=j?.filter||{},range=[filter.q&&'标题或编号：'+filter.q,filter.sender&&'发件人：'+filter.sender,filter.claimed&&filter.claimed!=='all'?(filter.claimed==='claimed'?'已领取':'未领取'):'全部领取状态',filter.from&&'起始 '+new Date(filter.from).toLocaleString(),filter.to&&'截止 '+new Date(filter.to).toLocaleString()].filter(Boolean).join(' · ');
  return `<section class="mail-cleanup" aria-label="跨页邮箱清理"><div class="card-head"><h3>跨页清理</h3>${button('查看清理任务','jobs',m.busy)}</div>${m.error?`<div class="hint-box" role="alert">${escape(m.error.message)}</div>`:''}${p&&!j?button('恢复邮箱清理任务','recover',m.busy):''}${j?`<div class="mail-cleanup-summary"><h4>${escape(labels[j.status])} · ${j.processed} / ${j.total} 封</h4><p>${escape(j.reason)}</p><p>${escape(range)}</p><p>未领取资产邮件 ${j.unclaimed} 封 · 金币 ${escape(j.gold)} · 附件 ${j.attachments} 件</p><small class="mono">任务 ${escape(j.id)}</small>${j.status==='conflict'?'<p role="alert">邮箱在预览后发生变化。已完成的批次保留，请按当前邮箱重新预览剩余邮件。</p>':''}${j.unclaimed&&!j.discardUnclaimed?'<p>此范围含未领取资产；勾选“同时丢弃未领取资产”后重新预览可清理这些邮件。</p>':''}<div class="actions">${m.auto?button('暂停处理','pause'):''}${j.status==='building'&&!m.auto?button('继续准备名单','prepare',m.busy):''}${j.status==='ready'?button('确认跨页清理','run',m.busy||!j.total||!!(j.unclaimed&&!j.discardUnclaimed)):''}${j.status==='running'&&!m.auto?button('继续清理','run',m.busy):''}${!terminal(j)&&!m.auto?button('结束此清理','cancel',m.busy):''}${button('刷新清理进度','read',m.busy)}</div></div>${m.items.length?`<div class="mail-cleanup-items"><h4>冻结名单 · 第 ${m.itemsPage+1} 页</h4><p>按名单顺序处理，已完成前 ${j.processed} 封。</p>${changesView(m.items.map(x=>x.change))}<div class="actions">${button('名单上一页','items-prev',m.busy||m.itemsPage===0)}${button('名单下一页','items-next',m.busy||!m.itemsNext)}</div></div>`:''}`:''}${m.jobs?`<div class="table-wrap"><table><thead><tr><th>原因</th><th>状态</th><th>已处理 / 总数</th><th>操作</th></tr></thead><tbody>${m.jobs.map(v=>`<tr><td>${escape(v.reason)}<small class="mono">${escape(v.id)}</small></td><td>${escape(labels[v.status])}</td><td>${v.processed} / ${v.total}</td><td><button type="button" class="button link" data-cleanup-open="${escape(v.id)}" ${m.busy?'disabled':''}>打开清理任务</button></td></tr>`).join('')||'<tr><td colspan="4">暂无清理任务</td></tr>'}</tbody></table></div><div class="actions">${button('任务上一页','jobs-prev',m.busy||m.jobsPage===0)}${button('任务下一页','jobs-next',m.busy||!m.jobsNext)}</div>`:''}</section>`;
 }
 document.addEventListener('click',e=>{const b=e.target.closest('[data-mail-cleanup],[data-cleanup-open]'),m=model();if(!b||!m)return;const action=b.dataset.mailCleanup;
  if(action==='pause'){m.auto=false;cancel('cleanup-advance');cancel('cleanup-create');m.busy=false;render();return;}
  if(m.busy)return;if(b.dataset.cleanupOpen){open(b.dataset.cleanupOpen);return;}
  if(action==='recover')recover();if(action==='read'&&m.job)open(m.job.id);if(action==='jobs')readJobs(true);
  if(action==='prepare'||action==='run'){m.auto=true;advance(action);}if(action==='cancel')advance('cancel');
  if(action==='items-prev'&&m.itemsPage>0){m.itemsPage--;readItems();}if(action==='items-next'&&m.itemsNext){m.itemsCursors=m.itemsCursors.slice(0,m.itemsPage+1);m.itemsCursors.push(m.itemsNext);m.itemsPage++;readItems();}
  if(action==='jobs-prev'&&m.jobsPage>0){m.jobsPage--;readJobs();}if(action==='jobs-next'&&m.jobsNext){m.jobsCursors=m.jobsCursors.slice(0,m.jobsPage+1);m.jobsCursors.push(m.jobsNext);m.jobsPage++;readJobs();}
 });
 return {create,view,reset,busy:()=>!!model()?.busy};
}
