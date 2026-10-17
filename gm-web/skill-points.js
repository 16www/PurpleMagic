import {escape} from './ui.js';

export function createSkillPoints({request,cancel,state,render}) {
 let current=null,pending={};
 const storageKey='soft-gm-skill-points-pending-v1';
 try {const v=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(v&&typeof v==='object'&&!Array.isArray(v))pending=v;} catch {}
 const save=()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(pending));}catch{}};
 const actions={add:'增加额外点数',subtract:'扣减额外点数',set:'设置额外点数',clear_remaining:'剩余点数归零',sync:'按 PVF 重算'};
 const targets={sp:'SP',tp:'TP',both:'SP / TP'};
 const button=(label,action,disabled=false)=>`<button type="button" class="button" data-points="${action}" ${disabled?'disabled':''}>${label}</button>`;
 const notice=e=>e?`<p class="hint-box" role="alert">${escape(e.message)}</p>`:'';
 const stamp=v=>new Date(v).toLocaleString('zh-CN',{hour12:false});
 const path=m=>`v1/characters/${encodeURIComponent(m.id)}/`;
 function reset(){for(const slot of ['points-read','points-log','points-preview','points-write'])cancel(slot);current=null;}
 function open(c){reset();current={id:c.characterId,accountId:c.accountId,name:c.name,scope:JSON.stringify([c.accountId,c.characterId]),data:null,loading:true,error:null,action:'add',target:'sp',amount:'',reason:'',preview:null,busy:false,result:null,rows:[],page:0,cursors:[''],next:'',logLoading:false,logError:null,epoch:0};read();log();}
 function read(){const m=current;if(!m)return;m.loading=true;m.error=null;
  request('points-read',path(m)+'skill-points',body=>{if(current!==m)return;m.data=body.data;m.loading=false;render();},error=>{if(current!==m)return;m.error=error;m.data=null;m.loading=false;render();});
 }
 function log(){const m=current;if(!m)return;m.logLoading=true;m.logError=null;const q=new URLSearchParams({limit:'10'});if(m.cursors[m.page])q.set('cursor',m.cursors[m.page]);
  request('points-log',path(m)+'skill-point-operations?'+q,body=>{if(current!==m)return;m.rows=body.data;m.next=body.page.nextCursor||'';m.logLoading=false;render();},error=>{if(current!==m)return;m.logError=error;m.logLoading=false;render();});
 }
 function preview(){const m=current;if(!m||m.busy||m.loading||!m.data||pending[m.scope])return;
  const amount=['sync','clear_remaining'].includes(m.action)?'0':m.amount,target=m.action==='sync'?'both':m.target;
  try{
   if(!/^(0|[1-9][0-9]{0,4})$/.test(amount)||Number(amount)>65535||(['add','subtract'].includes(m.action)&&amount==='0'))throw Error('请输入 0–65535 之间的整数，增减数量须大于 0。');
   if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500)throw Error('请填写操作原因，最多 500 字节。');
  }catch(e){m.error=e;render();return;}
  const body={accountId:m.accountId,action:m.action,target,amount,revision:m.data.revision,definitionRevision:m.data.definitionRevision,reason:m.reason},epoch=++m.epoch;
  m.busy=true;m.error=null;m.result=null;m.preview=null;render();
  request('points-preview',path(m)+'skill-point-operations/preview',response=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.preview={body:{...body,planToken:response.data.planToken},receipt:response.data.receipt};render();},error=>{if(current!==m||m.epoch!==epoch)return;m.busy=false;m.error=error;render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
 }
 function submit(){const m=current;if(!m||m.busy)return;let p=pending[m.scope];
  if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[m.scope]=p;save();}
  m.busy=true;m.error=null;render();
  request('points-write',path(m)+'skill-point-operations',body=>{delete pending[m.scope];save();if(current!==m)return;m.result=body.data;m.preview=null;m.amount='';m.reason='';m.busy=false;m.page=0;m.cursors=[''];read();log();render();},error=>{if(current!==m)return;m.busy=false;m.error=error;if([400,403,404,409,415,422].includes(error.status)){delete pending[m.scope];save();m.preview=null;}render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
 }
 function balances(b,d){return `<div class="points-balances">${[['sp','Sp','SP'],['tp','Tp','TP']].map(([id,key,label])=>`<article class="points-balance" aria-label="${label} 点数"><h3>${label} <strong data-points-remaining="${id}">${b['remaining'+key]}</strong><small>剩余点数</small></h3><dl class="detail-grid">${[['PVF 基础',d[id]],['额外点数',b['bonus'+key]],['当前总量',b['total'+key]],['已消耗',b['total'+key]-b['remaining'+key]]].map(([k,v])=>`<div><dt>${k}</dt><dd>${v}</dd></div>`).join('')}</dl></article>`).join('')}</div>`;}
 function changes(r){return `<div class="points-balances">${[['Sp','SP'],['Tp','TP']].map(([key,label])=>`<article class="points-change"><h4>${label}</h4><dl class="detail-grid">${[['额外','bonus'],['总量','total'],['剩余','remaining']].map(([name,field])=>`<div><dt>${name}</dt><dd>${r.before[field+key]} → ${r.after[field+key]}</dd></div>`).join('')}</dl></article>`).join('')}</div>`;}
 function view(){const m=current;if(!m)return '';const p=pending[m.scope]||m.preview,locked=!!pending[m.scope]||m.busy,ready=state.capabilities?.data.features['skillPoints.write'];
  return `<section aria-label="角色技能点"><div class="card-head"><div><h2>SP / TP</h2><p>${m.data?'Lv. '+m.data.level+' · 额外点数随等级基础一同计算':'技能点与调整记录'}</p></div>${button('刷新技能点与记录','refresh',locked)}</div>${notice(m.error)}${m.loading?'<p role="status">正在读取技能点…</p>':m.data?balances(m.data.balance,m.data.definition):''}${m.result?`<div class="hint-box" role="status" aria-label="技能点操作结果">${escape(actions[m.result.action])}已完成。SP 剩余 ${m.result.after.remainingSp}，TP 剩余 ${m.result.after.remainingTp}。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
  <form id="points-form" class="field-stack"><fieldset ${locked||m.loading||!m.data||!ready?'disabled':''}><div class="currency-fields"><label>技能点操作<select name="action" aria-label="技能点操作">${Object.entries(actions).map(([id,label])=>`<option value="${id}" ${m.action===id?'selected':''}>${label}</option>`).join('')}</select></label>${m.action!=='sync'?`<label>点数类型<select name="target" aria-label="点数类型">${[['sp','SP'],['tp','TP']].map(([id,label])=>`<option value="${id}" ${m.target===id?'selected':''}>${label}</option>`).join('')}</select></label>`:''}${!['sync','clear_remaining'].includes(m.action)?`<label>${m.action==='set'?'目标额外点数':'调整数量'}<input name="amount" aria-label="技能点数量" inputmode="numeric" maxlength="5" value="${escape(m.amount)}" required></label>`:''}<label class="currency-reason">技能点操作原因<input name="reason" maxlength="500" value="${escape(m.reason)}" required></label></div><p class="hint">${m.action==='clear_remaining'?'所选剩余点数将计入已消耗点数，已学技能保持。':m.action==='sync'?'按当前 PVF 和额外点数重算 SP / TP，保留已消耗点数。':'调整额外点数后，按当前 PVF 重算 SP / TP 并保留已消耗点数。'} 提交时角色需离线。</p><button type="submit" class="button primary">预览技能点变更</button></fieldset></form>
  ${p?`<div class="currency-confirm" aria-label="技能点变更确认"><h3>${escape(actions[p.body.action])} · ${escape(targets[p.body.target])}</h3><p>${escape(m.name)} · ${escape(p.body.reason)}</p>${changes(p.receipt)}<details><summary>PVF 版本与等级</summary><p>Lv. ${p.receipt.definition.level}</p><small class="mono">${escape(p.receipt.definition.pvfChecksum)}</small></details><div class="actions">${button(m.busy?'提交中…':pending[m.scope]?'重试技能点并取得回执':'确认技能点提交','submit',m.busy)}${!pending[m.scope]&&!m.busy?button('返回修改技能点','cancel'):''}</div></div>`:''}
  <div class="card-head currency-ledger-head"><h2>技能点调整记录</h2><span>第 ${m.page+1} 页</span></div>${notice(m.logError)}${m.logLoading?'<p>正在读取记录…</p>':`<div class="points-history">${m.rows.map(r=>`<details><summary>${escape(actions[r.action])} · ${escape(targets[r.target])}<small>${escape(stamp(r.createdAt))}</small></summary><p>${escape(r.reason)} · ${escape(r.actor)}</p>${changes(r)}<small class="mono">回执 ${escape(r.requestId)}</small></details>`).join('')||'<p class="empty-state">暂无技能点调整记录</p>'}</div>`}<div class="table-footer"><span>本页 ${m.rows.length} 条</span><div class="actions">${button('上一页技能点记录','prev',m.logLoading||m.page===0)}${button('下一页技能点记录','next',m.logLoading||!m.next)}</div></div></section>`;
 }
 function edit(e){const m=current;if(!m||!e.target.closest('#points-form')||m.busy||pending[m.scope])return;m[e.target.name]=e.target.value;m.preview=null;++m.epoch;if(e.type==='change'&&e.target.tagName==='SELECT')render();else document.querySelector('[aria-label="技能点变更确认"]')?.remove();}
 document.addEventListener('input',edit);document.addEventListener('change',edit);
 document.addEventListener('submit',e=>{if(e.target.id==='points-form'){e.preventDefault();preview();}});
 document.addEventListener('click',e=>{const action=e.target.closest('[data-points]')?.dataset.points,m=current;if(!action||!m)return;switch(action){case 'submit':submit();break;case 'cancel':m.preview=null;render();break;case 'refresh':if(locked())return;m.preview=null;m.result=null;read();m.page=0;m.cursors=[''];log();render();break;case 'prev':if(!m.logLoading&&m.page>0){m.page--;log();render();}break;case 'next':if(!m.logLoading&&m.next){m.cursors=m.cursors.slice(0,m.page+1);m.cursors.push(m.next);m.page++;log();render();}break;}function locked(){return m.busy||pending[m.scope];}});
 return {open,reset,view};
}
