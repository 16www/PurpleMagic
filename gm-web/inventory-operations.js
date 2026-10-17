import {escape} from './ui.js';

const names={count:'数量',qualitySeed:'品级种子',reinforce:'强化',amplifyType:'增幅类型',amplifyValue:'增幅值',forging:'锻造',expiresAt:'到期时间',bound:'绑定',durability:'耐久',max_durability:'最大耐久',serial:'实例编号',seal:'封装'};
const date=v=>!v||v.startsWith('0001')?'永久':new Date(v).toLocaleString('zh-CN',{hour12:false});
const button=(label,action,disabled=false)=>`<button type="button" class="button" data-inventory-operation="${action}" ${disabled?'disabled':''}>${label}</button>`;
const artifactColors={red:'红色',blue:'蓝色',green:'绿色'};
export function inventoryChangesView(changes){
  return `<div class="inventory-changes">${changes.map(c=>{
    const before={...c.before.attributes,count:c.before.count,expiresAt:date(c.before.expiresAt),bound:c.before.bound?'已绑定':'未绑定'},after=c.after?{...c.after.attributes,count:c.after.count,expiresAt:date(c.after.expiresAt),bound:c.after.bound?'已绑定':'未绑定'}:null;
    const changed=after?(c.container==='shared'?['count']:Object.keys(after)).filter(k=>after[k]!==before[k]):['count'];
    return `<article class="inventory-change"><h4>${escape(c.before.name||c.before.itemId)} <small>槽位 ${escape(c.slot)}</small></h4><small class="mono">${escape(c.before.itemId)}${c.before.attributes.serial?' · '+escape(c.before.attributes.serial):''}</small>${after?`<dl class="inventory-diff">${changed.map(k=>`<div><dt>${escape(names[k]||k)}</dt><dd>${escape(before[k]??'未知')} → <strong>${escape(after[k]??'未知')}</strong></dd></div>`).join('')||'<div><dt>属性</dt><dd>保持当前值</dd></div>'}</dl>`:`<p class="inventory-delete">删除整格，共 ${escape(c.before.count)} 件</p>${c.before.pet?`<p>同时删除宠物成长记录：${escape(c.before.pet.name||c.before.name)} · 等级 ${escape(c.before.pet.level)} · 经验 ${escape(c.before.pet.experience)}${c.before.pet.equippedKey===c.before.pet.petKey?' · 清除当前出战关联':''}</p>`:''}`}${c.before.petArtifact?`<p>${after?'同步更新':'同时移除'}${escape(artifactColors[c.before.petArtifact.color])}宠物装备记录</p>`:''}</article>`;
  }).join('')}</div>`;
}

export function createInventoryOperations({request,cancel,state,render,refresh}){
  const storageKey='soft-gm-inventory-pending-v1';let pending={};const reads=new Set();
  try{pending=JSON.parse(sessionStorage.getItem(storageKey)||'{}');if(!pending||typeof pending!=='object'||Array.isArray(pending))pending={};}catch{}
  const save=()=>sessionStorage.setItem(storageKey,JSON.stringify(pending));
  const model=()=>state.detail?.resources?state.detail.resources.inventoryOperation??={selected:new Set(),mode:'',items:null,values:{},reason:'',busy:false,epoch:0,error:null,preview:null,result:null}:null;
  const character=()=>state.detail?.data?.character;
  const scope=c=>JSON.stringify([c.accountId,c.characterId]);
  const locked=()=>{const m=model(),c=character();return !!(m?.busy||(c&&pending[scope(c)]));};
  const accountContainer=container=>['account-cargo','shared'].includes(container);
  const supported=(container,slot)=>container!=='equipped'||slot===undefined||(slot>=0&&slot<=32);
  const configurable=(container,slot)=>supported(container,slot)&&container!=='pets'&&!(container==='equipped'&&slot===26);
  function cancelReads(){for(const slot of reads)cancel(slot);reads.clear();cancel('inventory-preview');}
  function reset(){cancelReads();cancel('inventory-write');const m=model();if(m){m.epoch++;m.busy=false;m.preview=null;}}
  function clearSelection(){cancelReads();const m=model(),c=character();if(m){m.epoch++;m.selected.clear();m.items=null;m.mode='';m.values={};m.preview=null;if(!c||!pending[scope(c)])m.busy=false;}}
  function checkbox(row){const m=model(),container=state.detail?.resources.container;return `<input type="checkbox" data-inventory-select="${row.slot}" aria-label="选择槽位 ${row.slot}" ${m?.selected.has(row.slot)?'checked':''} ${locked()||!supported(container,row.slot)?'disabled':''}>`;}
  async function prepare(mode,slot){
    const owner=state.detail,m=model(),c=character();if(!m||!c||locked())return;
    if(slot!==undefined)m.selected=new Set([slot]);
    if(!m.selected.size){m.error=new Error('请选择物品。');render();return;}
    cancelReads();const epoch=++m.epoch,container=owner.resources.container,queue=[...m.selected],loaded=[];
    const live=()=>state.detail===owner&&m.epoch===epoch;
    m.mode=mode;m.values={};m.items=null;m.preview=null;m.error=null;m.result=null;m.busy=true;render();
    async function worker(){
      while(queue.length&&live()){
        const slot=queue.shift(),name='inventory-options-'+slot;reads.add(name);
        await request(name,`v1/characters/${encodeURIComponent(c.characterId)}/inventory/${encodeURIComponent(container)}/${slot}/options`,body=>{if(live())loaded.push(body.data);},e=>{if(live())m.error=e;});reads.delete(name);
      }
    }
    await Promise.all(Array.from({length:Math.min(4,queue.length)},worker));if(!live())return;
    m.busy=false;if(!m.error){m.items=loaded.sort((a,b)=>a.item.slot-b.item.slot);const unavailable=m.items.find(i=>!i.canDelete);if(unavailable)m.error=new Error(unavailable.item.petLinkError||unavailable.item.petArtifactLinkError||'所选容器需要专用管理操作。');}
    render();
  }
  function fields(m){
    if(!m.items?.length)return '';
    const equipment=m.items.map(i=>i.configuration?.equipment),allEquipment=equipment.every(Boolean),quantity=m.items.every(i=>i.quantity),expiration=m.items.every(i=>i.configuration?.expiration);
    const numeric=(id,label,max,min=id==='count'||id==='expirationDays'?1:0)=>`<label>${label}<input name="${id}" type="number" min="${min}" max="${max}" step="1" value="${escape(m.values[id]||'')}" placeholder="不修改 · 上限 ${max}"></label>`;
    const select=(id,label,options)=>`<label>${label}<select name="${id}" aria-label="${label}"><option value="">不修改</option>${options.map(([value,text])=>`<option value="${value}" ${m.values[id]===String(value)?'selected':''}>${text}</option>`).join('')}</select></label>`;
    let html='';
    if(allEquipment){
      html+=select('qualityMode','品级',[[0,'随机品级'],[1,'最上级（100%）']]);
      if(equipment.every(e=>e.canUpgrade))html+=numeric('upgradeLevel','强化等级',Math.min(...equipment.map(e=>e.maxUpgradeLevel)));
      if(equipment.every(e=>e.canAmplify))html+=select('amplifyType','增幅类型',[[0,'清除增幅'],[1,'体力'],[2,'精神'],[3,'力量'],[4,'智力']]);
      if(equipment.every(e=>e.canForge))html+=numeric('forgingLevel','锻造等级',Math.min(...equipment.map(e=>e.maxForgingLevel)));
    }
    if(quantity){const max=m.items.reduce((a,i)=>BigInt(i.quantity.max)<a?BigInt(i.quantity.max):a,2147483647n),min=Math.max(...m.items.map(i=>Number(i.quantity.min??1)));html+=numeric('count','物品数量',max.toString(),min);}
    if(expiration)html+=numeric('expirationDays','有效天数',Math.min(...m.items.map(i=>i.configuration.expiration.maxDays)));
    return html||'<p>所选物品没有共同的可修改属性，请选择同类物品。</p>';
  }
  function preview(){
    const owner=state.detail,m=model(),c=character();if(!m||!m.items||locked())return;
    if(!m.reason.trim()||new TextEncoder().encode(m.reason).length>500){m.error=new Error('请填写操作原因，最多 500 字节。');render();return;}
    const options={};
    for(const [k,v] of Object.entries(m.values)){if(v==='')continue;if(!/^\d+$/.test(v)||!Number.isSafeInteger(Number(v))){m.error=new Error('属性值需要填写整数。');render();return;}options[k]=k==='count'?BigInt(v).toString():Number(v);}
    if(m.mode==='configure'&&!Object.keys(options).length){m.error=new Error('请填写需要修改的属性。');render();return;}
    const body={accountId:c.accountId,action:m.mode,items:m.items.map(({item:i})=>({container:i.container,slot:i.slot,revision:i.revision,...(i.relatedRevision?{relatedRevision:i.relatedRevision}:{})})),pvfChecksum:m.items[0].item.pvfChecksum,reason:m.reason,...(m.mode==='configure'?{options}:{})};
    m.error=null;m.busy=true;render();
    request('inventory-preview',`v1/characters/${encodeURIComponent(c.characterId)}/inventory-operations/preview`,response=>{if(state.detail!==owner)return;m.busy=false;m.preview={body:{...body,planToken:response.data.planToken},receipt:response.data.receipt};render();},e=>{if(state.detail!==owner)return;m.busy=false;m.error=e;render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken},body:JSON.stringify(body)});
  }
  function submit(){
    const owner=state.detail,m=model(),c=character();if(!m||!c||m.busy)return;const id=scope(c);let p=pending[id];
    if(!p){if(!m.preview)return;p={...m.preview,key:crypto.randomUUID()};pending[id]=p;try{save();}catch{delete pending[id];m.error=new Error('浏览器无法保存操作编号，请启用会话存储后重试。');render();return;}}
    m.error=null;m.busy=true;render();
    request('inventory-write',`v1/characters/${encodeURIComponent(c.characterId)}/inventory-operations`,response=>{
      delete pending[id];try{save();}catch{}if(state.detail!==owner)return;m.busy=false;m.preview=null;m.result=response.data;refresh();render();
    },e=>{if(state.detail!==owner)return;m.busy=false;m.error=e;if([400,403,404,409,415,422].includes(e.status)){delete pending[id];try{save();}catch{}m.preview=null;}render();},{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':state.capabilities.data.csrfToken,'Idempotency-Key':p.key},body:JSON.stringify(p.body)});
  }
  function view(){
    const m=model(),c=character();if(!m||!c)return '';const p=pending[scope(c)]||m.preview,isPending=!!pending[scope(c)],accountOwned=accountContainer(state.detail.resources.container),enabled=state.capabilities?.data.features[accountOwned?'inventory.account.write':'inventory.write'];
    return `<section class="inventory-operations" aria-label="物品操作"><div class="card-head"><h3>物品操作</h3><span>已选 ${m.selected.size} 格</span></div>${m.error?`<div class="hint-box" role="alert">${escape(m.error.message)}</div>`:''}${m.result?`<div class="hint-box" role="status">${m.result.action==='delete'?'删除':'配置'}已完成，共 ${m.result.changes.length} 格。<small class="mono">回执 ${escape(m.result.requestId)}</small></div>`:''}
    ${accountOwned?'<p>修改将对该账号所有角色生效，记录保存在“账号物品记录”。</p>':''}<div class="actions">${button('配置所选','configure',locked()||!enabled||!configurable(state.detail.resources.container)||[...m.selected].some(slot=>!configurable(state.detail.resources.container,slot)))}${button('删除所选','delete',locked()||!enabled)}${button('选择本页物品','select-page',locked()||!supported(state.detail.resources.container))}${button('清空物品选择','clear',locked())}</div>${m.busy&&!isPending?'<p role="status">正在处理所选物品…</p>':''}
    ${m.items?`<form id="inventory-operation-form"><fieldset ${locked()?'disabled':''}><h4>${m.mode==='delete'?'删除所选整格物品':'配置所选物品'}</h4><p>${m.items.map(i=>escape(i.item.name||i.item.itemId)+'（槽位 '+i.item.slot+'）').join('、')}</p>${m.mode==='configure'?`<div class="inventory-fields">${fields(m)}</div>`:''}<label>物品操作原因<input name="reason" maxlength="500" required value="${escape(m.reason)}" placeholder="例如：装备修复、补偿调整"></label><button type="submit" class="button primary">预览物品变更</button></fieldset></form>`:''}
    ${p?`<div class="inventory-confirm" data-inventory-confirm aria-label="物品变更确认"><h3>${isPending?'待确认的物品操作':'确认物品变更'}</h3><p><strong>${escape(c.name)}</strong> · ${escape(p.body.reason)}</p>${inventoryChangesView(p.receipt.changes)}<div class="actions">${button(m.busy?'提交中…':isPending?'重试并取得物品回执':'确认物品提交','submit',m.busy)}${!isPending?button('返回修改物品操作','cancel',m.busy):''}</div></div>`:''}</section>`;
  }
  document.addEventListener('change',e=>{const m=model();if(!m)return;
    if(e.target.matches('[data-inventory-select]')&&!locked()){const slot=Number(e.target.dataset.inventorySelect);e.target.checked?m.selected.add(slot):m.selected.delete(slot);m.preview=null;m.items=null;m.error=null;render();}
    else if(e.target.closest('#inventory-operation-form')){if(e.target.name==='reason')m.reason=e.target.value;else m.values[e.target.name]=e.target.value;m.preview=null;document.querySelector('[data-inventory-confirm]')?.remove();}
  });
  document.addEventListener('input',e=>{const m=model();if(m&&e.target.closest('#inventory-operation-form')){if(e.target.name==='reason')m.reason=e.target.value;else m.values[e.target.name]=e.target.value;m.preview=null;document.querySelector('[data-inventory-confirm]')?.remove();}});
  document.addEventListener('submit',e=>{if(e.target.id==='inventory-operation-form'){e.preventDefault();preview();}});
  document.addEventListener('click',e=>{const b=e.target.closest('button'),m=model();if(!b||!m)return;
    if(b.dataset.inventoryEdit!==undefined)prepare('configure',Number(b.dataset.inventoryEdit));
    const action=b.dataset.inventoryOperation;if(!action)return;
    if(action==='submit')submit();else if(!locked()){
      if(action==='configure'||action==='delete')prepare(action);
      if(action==='select-page'){m.selected=new Set(state.detail.resources.rows.filter(r=>supported(state.detail.resources.container,r.slot)).map(r=>r.slot));m.items=null;m.preview=null;render();}
      if(action==='clear'){clearSelection();render();}
      if(action==='cancel'){m.preview=null;render();}
    }
  });
  return {view,checkbox,reset,clearSelection,supported,configurable};
}
