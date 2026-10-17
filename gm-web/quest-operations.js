import {escape} from './ui.js';

export const questActions = {accept:'接取任务','give-up':'放弃任务',ready:'标记可交',complete:'标记完成（不发奖励）',reset:'恢复未完成 / 重置链路'};
export const questGrade = value => ({epic:'主线',normal:'普通',side:'支线',sub:'支线',achievement:'成就',daily:'每日',system:'系统',training:'训练',realization:'觉醒',mission:'使命'}[String(value||'').replace(/[\[\]`]/g,'').trim()] || value || '—');
export const questStatus = value => ({unaccepted:'未接取',active:'进行中',accepted:'进行中',completed:'已完成',complete:'已完成',cleared:'已完成',finished:'已完成',done:'已完成'}[value] || value || '—');
const button = (label,action,disabled=false) => `<button type="button" class="button" data-quest-operation="${action}" ${disabled?'disabled':''}>${label}</button>`;
const notice = e => e ? `<div class="hint-box" role="alert">${escape(e.message)}</div>` : '';
export function questChangesView(changes) {
  return `<div class="table-wrap"><table><thead><tr><th>任务编号</th><th>变更前</th><th>变更后</th><th>进度</th><th>结果</th></tr></thead><tbody>${changes.map(c=>`<tr><td class="mono">${escape(c.questId)}</td><td>${escape(questStatus(c.before))}</td><td>${escape(questStatus(c.after))}</td><td>${escape(c.progressBefore)} → ${escape(c.progressAfter)}</td><td>${c.changed?'变更':'保持'}</td></tr>`).join('')}</tbody></table></div>`;
}

export function createQuestOperations({request,cancel,state,render,refresh}) {
  const storageKey='soft-gm-quest-pending-v1';
  let pending={};
  try { pending=JSON.parse(sessionStorage.getItem(storageKey)||'{}'); if(!pending||Array.isArray(pending)||typeof pending!=='object')pending={}; } catch { pending={}; }
  const save=()=>sessionStorage.setItem(storageKey,JSON.stringify(pending));
  const model=()=>state.detail?.resources ? state.detail.resources.operation??={selected:new Set(),action:'complete',reason:'',busy:false,error:null,preview:null,result:null} : null;
  const identity=()=>state.detail?.data?.character;
  const scope=c=>JSON.stringify([c.accountId,c.characterId]);
  const current=owner=>state.detail===owner;
  function reset(){cancel('quest-preview');cancel('quest-write');const m=model();if(m){m.busy=false;m.preview=null;}}
  function clearSelection(){cancel('quest-preview');const m=model();if(m){m.selected.clear();m.preview=null;if(!pending[scope(identity())])m.busy=false;}}
  function locked(){const c=identity(),m=model();return !!(m?.busy || (c&&pending[scope(c)]));}
  function checkbox(row){const m=model();return `<input type="checkbox" data-quest-select="${escape(row.questId)}" aria-label="选择任务 ${escape(row.questId)}" ${m?.selected.has(row.questId)?'checked':''} ${row.definitionKnown===false||locked()?'disabled':''}>`;}
  function preview(){
    const owner=state.detail,m=model(),c=identity();if(!c||!m||locked())return;
    if(!m.selected.size){m.error=new Error('请选择任务。');render();return;}
    if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500){m.error=new Error('请填写操作原因，最多 500 字节。');render();return;}
    const body={accountId:c.accountId,action:m.action,questIds:[...m.selected],reason:m.reason};
    m.busy=true;m.error=null;m.result=null;render();
    request('quest-preview',`v1/characters/${encodeURIComponent(c.characterId)}/quest-operations/preview`,response=>{
      if(!current(owner))return;m.busy=false;m.preview={body:{...body,revision:response.data.revision,pvfChecksum:response.data.pvfChecksum},receipt:response.data};render();
    },e=>{if(current(owner)){m.busy=false;m.error=e;render();}}, {method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
  }
  function submit(){
    const owner=state.detail,m=model(),c=identity();if(!m||!c||m.busy)return;
    const id=scope(c);let p=pending[id];
    if(!p){
      if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[id]=p;
      try{save();}catch{delete pending[id];m.error=new Error('浏览器无法保存操作编号，请启用会话存储后重试。');render();return;}
    }
    m.busy=true;m.error=null;render();
    request('quest-write',`v1/characters/${encodeURIComponent(c.characterId)}/quest-operations`,response=>{
      delete pending[id];try{save();}catch{}
      if(!current(owner))return;m.busy=false;m.preview=null;m.result=response.data;m.selected.clear();refresh();render();
    },e=>{
      if(!current(owner))return;m.busy=false;m.error=e;
      // Definitive request rejection permits a new preview. Transport and server
      // failures retain the original command so a lost commit can be recovered.
      if([400,403,404,409,415,422].includes(e.status)){delete pending[id];try{save();}catch{}m.preview=null;}
      render();
    }, {method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
  }
  function view(){
    const m=model(),c=identity();if(!m||!c)return '';
    const p=pending[scope(c)]||m.preview,isPending=!!pending[scope(c)],ready=state.capabilities?.data.features['quests.write'];
    return `<section class="quest-operations" aria-label="任务操作"><div class="card-head"><h3>任务操作</h3><span>已选 ${m.selected.size} 项</span></div>${notice(m.error)}${m.result?`<div class="hint-box" role="status">${escape(questActions[m.result.action])}已完成，${m.result.changes.filter(c=>c.changed).length} 项变更。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
    <form id="quest-operation-form"><fieldset ${locked()||!ready?'disabled':''}><div class="quest-fields"><label>任务操作类型<select name="action" aria-label="任务操作类型">${Object.entries(questActions).map(([id,label])=>`<option value="${id}" ${m.action===id?'selected':''}>${label}</option>`).join('')}</select></label><label>任务操作原因<input name="reason" maxlength="500" required placeholder="例如：修复卡住的任务进度" value="${escape(m.reason)}"></label></div><div class="actions"><button type="submit" class="button primary">预览任务变更</button>${button('选择本页','select-page')}${button('清空选择','clear')}</div></fieldset></form>
    ${p?`<div class="quest-confirm" data-quest-confirm aria-label="任务变更确认"><h3>${isPending?'待确认的任务操作':'确认任务变更'}</h3><p><strong>${escape(c.name)}</strong> · ${escape(questActions[p.body.action])}</p><p>${escape(p.body.reason)}</p>${p.body.action==='reset'?'<p>恢复任务后可以重新推进；关联链路的变化列在下方。</p>':''}${questChangesView(p.receipt.changes)}<div class="actions">${button(m.busy?'提交中…':isPending?'重试并取得任务回执':'确认任务提交','submit',m.busy)}${!isPending?button('返回修改任务操作','cancel',m.busy):''}</div></div>`:''}</section>`;
  }
  document.addEventListener('change',e=>{
    const m=model();if(!m)return;
    if(e.target.matches('[data-quest-select]')){if(locked())return;const id=e.target.dataset.questSelect;e.target.checked?m.selected.add(id):m.selected.delete(id);m.preview=null;m.error=null;render();}
    else if(e.target.closest('#quest-operation-form')){m[e.target.name]=e.target.value;m.preview=null;document.querySelector('[data-quest-confirm]')?.remove();}
  });
  document.addEventListener('input',e=>{const m=model();if(m&&e.target.closest('#quest-operation-form')){m[e.target.name]=e.target.value;m.preview=null;document.querySelector('[data-quest-confirm]')?.remove();}});
  document.addEventListener('submit',e=>{if(e.target.id==='quest-operation-form'){e.preventDefault();preview();}});
  document.addEventListener('click',e=>{
    const action=e.target.closest('[data-quest-operation]')?.dataset.questOperation,m=model();if(!action||!m)return;
    if(action==='submit')submit();
    else if(!locked()){
      if(action==='select-page'){m.selected=new Set(state.detail.resources.rows.filter(r=>r.definitionKnown!==false).map(r=>r.questId));m.preview=null;}
      if(action==='clear'){m.selected.clear();m.preview=null;}
      if(action==='cancel')m.preview=null;
      render();
    }
  });
  return {view,checkbox,reset,clearSelection};
}
