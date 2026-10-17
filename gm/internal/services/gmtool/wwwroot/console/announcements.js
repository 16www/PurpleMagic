import {$,escape} from './ui.js';

const blank=()=>({title:'',body:'',category:'综合',date:''});
const button=(label,action,disabled=false)=>`<button type="button" class="button" data-news="${action}" ${disabled?'disabled':''}>${label}</button>`;
export function createAnnouncements({request,cancel,render,state}){
 const hub=location.pathname.startsWith('/gm/');
 let model={loaded:false,loading:false,busy:false,news:[],revision:'',csrf:'',selected:-1,draft:blank(),dirty:false,error:'',message:'',conflict:false};
 const active=()=>state.auth==='ready'&&state.route==='gateway';
 const paint=()=>{if(active())render();};
 function leave(){cancel('news-load');model.loading=false;}
 function reset(){leave();model={loaded:false,loading:false,busy:false,news:[],revision:'',csrf:'',selected:-1,draft:blank(),dirty:false,error:'',message:'',conflict:false};}
 function load(force=false){
  if(!hub||model.busy||model.loaded&&!force)return;
  model.loading=true;model.error='';paint();
  request('news-load','/admin/api/state',response=>{
   model.news=response.published.news||[];model.revision=response.newsRevision||'';model.csrf=response.csrf;
   model.loaded=true;model.loading=false;model.conflict=false;model.selected=-1;model.draft=blank();model.dirty=false;paint();
  },error=>{model.loading=false;model.error=error.message;paint();});
 }
 function select(index){model.selected=index;model.draft={...(model.news[index]||blank())};model.dirty=false;model.error='';model.message='';paint();}
 function publish(next,selected){
  if(model.busy||model.conflict||!model.revision)return;
  if(next.length>30){model.error='最多发布 30 条公告。';paint();return;}
  const encoded=new TextEncoder();
  if(next.some(n=>!n.title.trim()||encoded.encode(n.title).length>160||encoded.encode(n.body).length>12000||encoded.encode(n.date).length>32)){
   model.error='标题最多 160 字节，正文最多 12000 字节，日期最多 32 字节。';paint();return;
  }
  if(encoded.encode(JSON.stringify(next)).length>128*1024){model.error='公告列表内容过长，请缩短正文。';paint();return;}
  model.busy=true;model.error='';model.message='';paint();
  request('news-write','/admin/api/news',response=>{
   model.news=next;model.revision=response.newsRevision;model.busy=false;model.selected=selected;
   model.draft={...(next[selected]||blank())};model.dirty=false;model.message='公告已发布，登录器下次检查更新时展示。';paint();
  },error=>{model.busy=false;model.error=error.message;model.conflict=error.status===409;paint();},
   {method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':model.csrf,'If-Match':`"${model.revision}"`},body:JSON.stringify(next)});
 }
 function dialog(title,body,action,label){
  $('#dialog').innerHTML=`<div class="dialog-head"><h2 id="dialog-title">${title}</h2>${button('关闭','close')}</div>${body}${action?`<div class="actions">${button('取消','close')}<button class="button primary" data-news="${action}">${label}</button></div>`:''}`;$('#dialog').showModal();
 }
 function view(){
  const heading=`<div class="page-heading"><h1>网关与更新</h1>${hub?'<a class="button" href="/admin/">更新源与版本管理</a>':''}</div>`;
  if(!hub)return heading+'<section class="card"><h2>登录器公告</h2><p class="hint">通过私有 Hub 的 GM 工作台管理公告。</p></section>';
  const disabled=model.busy||model.loading||!model.loaded, d=model.draft;
  return heading+`<section class="card"><div class="card-head"><h2>登录器公告</h2><div class="actions"><span class="hint">${model.news.length} / 30 条</span>${button('重新载入','reload',disabled)}${button('新增公告','new',disabled||model.news.length>=30)}</div></div>${model.message?`<p class="hint" role="status">${escape(model.message)}</p>`:''}${model.error?`<div class="hint-box" role="alert">${escape(model.error)}${!model.loaded?button('重试','retry'):''}</div>`:''}${model.loading?'<p class="hint">正在读取公告…</p>':''}<div class="news-layout"><div class="news-list" aria-label="已发布公告">${model.news.map((n,i)=>`<button class="news-entry ${model.selected===i?'selected':''}" data-news="select" data-index="${i}" ${disabled?'disabled':''}><span class="badge">${escape(n.category)}</span><strong>${escape(n.title)}</strong><small>${escape(n.date)}</small></button>`).join('')||'<p class="hint">暂无公告</p>'}</div><form id="news-form"><fieldset ${disabled?'disabled':''}><div class="card-head"><h3>${model.selected<0?'新增公告':'编辑公告'}</h3>${model.selected>=0?`<div class="actions">${button('上移','up',model.selected===0||model.dirty||model.conflict)}${button('下移','down',model.selected===model.news.length-1||model.dirty||model.conflict)}${button('删除','remove',model.conflict)}</div>`:''}</div><label>公告标题<input name="title" required maxlength="160" value="${escape(d.title)}"></label><div class="news-meta"><label>公告分类<select name="category" aria-label="公告分类">${['综合','更新','指南'].map(c=>`<option ${c===d.category?'selected':''}>${c}</option>`).join('')}</select></label><label>显示日期<input name="date" maxlength="32" value="${escape(d.date)}" placeholder="10 / 15"></label></div><label>公告正文<textarea name="body" rows="9" maxlength="12000">${escape(d.body)}</textarea></label><div class="actions">${button('预览','preview')}<button class="button primary" type="submit" ${model.conflict||!model.revision?'disabled':''}>${model.busy?'正在发布…':'发布公告'}</button></div></fieldset></form></div></section>`;
 }
 document.addEventListener('input',event=>{if(!event.target.closest('#news-form'))return;model.draft[event.target.name]=event.target.value;model.dirty=true;document.querySelectorAll('[data-news="up"],[data-news="down"]').forEach(button=>button.disabled=true);});
 document.addEventListener('submit',event=>{
  if(event.target.id!=='news-form')return;event.preventDefault();const next=model.news.map(n=>({...n}));const index=model.selected<0?next.length:model.selected;
  next[index]={...model.draft,title:model.draft.title.trim()};publish(next,index);
 });
 document.addEventListener('click',event=>{
  const el=event.target.closest('[data-news]');if(!el||el.disabled)return;const action=el.dataset.news;
  if(action==='close'){$('#dialog').close();return;}if(!active())return;
  if(action==='select')select(Number(el.dataset.index));
  else if(action==='new')select(-1);
  else if(action==='preview')dialog('公告预览',`<article class="news-preview"><p class="hint">${escape(model.draft.category)} · ${escape(model.draft.date)}</p><h3>${escape(model.draft.title)}</h3><div>${escape(model.draft.body)}</div></article>`);
  else if(action==='reload'){
   if(model.dirty)dialog('重新载入公告','<p>重新载入将使用已发布内容替换当前编辑。</p>','reload-confirm','重新载入');else load(true);
  }else if(action==='retry')load(true);
  else if(action==='reload-confirm'){$('#dialog').close();load(true);}
  else if(action==='remove')dialog('删除公告',`<p>${escape(model.news[model.selected].title)}</p>`,'remove-confirm','删除并发布');
  else if(action==='remove-confirm'){$('#dialog').close();publish(model.news.filter((_,i)=>i!==model.selected),-1);}
  else if(action==='up'||action==='down'){
   const next=[...model.news],index=model.selected,other=index+(action==='up'?-1:1);[next[index],next[other]]=[next[other],next[index]];publish(next,other);
  }
 });
 return {view,load,leave,reset};
}
