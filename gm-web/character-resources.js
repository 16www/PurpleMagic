import {createMailboxOperations,mailboxChangesView} from './mailbox-operations.js';
import {createVault} from './vault.js';
import {createContainers} from './containers.js';
import {createGold} from './gold.js';
import {createSkillPoints} from './skill-points.js';
import {createLevel} from './level.js';
import {createProfession} from './profession.js';
import {createExpertJob} from './expert-job.js';
import {$,escape} from './ui.js';
import {renderItemDetail} from './item-detail-view.js';
import {createInventoryOperations,inventoryChangesView} from './inventory-operations.js';
import {createQuestOperations,questActions,questStatus,questGrade,questChangesView} from './quest-operations.js';
const containers=[['equipment','背包装备'],['consumables','消耗品'],['materials','材料'],['quest-items','任务物品'],['profession','副职业材料'],['quickbar','快捷栏'],['avatars','装扮'],['pets','宠物'],['pet-equipment','宠物装备'],['pet-consumables','宠物消耗品'],['medals','勋章与守护珠'],['equipped','身上穿戴'],['warehouse','个人仓库'],['account-cargo','账号金库'],['shared','账号晶块与灵魂']];
const labels={qualitySeed:'品级种子',reinforce:'强化等级',amplifyType:'增幅类型',amplifyValue:'增幅数值',forging:'锻造等级',durability:'当前耐久',max_durability:'最大耐久',seal:'封装状态',serial:'实例编号',enchant_id:'附魔 ID',enchant_card_id:'附魔卡 ID',avatar_option_value:'装扮选项',emblem_0:'徽章 1',emblem_1:'徽章 2',emblem_2:'徽章 3'};
const stamp=v=>v?new Date(v).toLocaleString('zh-CN',{hour12:false}):'—';
const button=(label,action,disabled=false)=>'<button type="button" class="button" data-resource-action="'+action+'" '+(disabled?'disabled':'')+'>'+escape(label)+'</button>';
const field=(name,value)=>'<div><dt>'+escape(name)+'</dt><dd>'+escape(value??'未知')+'</dd></div>';
const error=e=>'<div class="hint-box" role="alert">'+escape(e.message)+(e.requestId?'<p class="mono">请求编号：'+escape(e.requestId)+'</p>':'')+'</div>';

export function createCharacterResources({request,cancel,state,renderDialog}){
  const mailbox=createMailboxOperations({request,cancel,state,render:renderDialog,refresh:fresh});
  const vault=createVault({request,cancel,state,render:renderDialog});
  const capacity=createContainers({request,cancel,state,render:renderDialog});
  const gold=createGold({request,cancel,state,render:renderDialog});
  const points=createSkillPoints({request,cancel,state,render:renderDialog});
  const level=createLevel({request,cancel,state,render:renderDialog});
  const profession=createProfession({request,cancel,state,render:renderDialog});
  const expertJob=createExpertJob({request,cancel,state,render:renderDialog});
  const inventory=createInventoryOperations({request,cancel,state,render:renderDialog,refresh:fresh});
  const quests=createQuestOperations({request,cancel,state,render:renderDialog,refresh:fresh});
  function model(){const d=state.detail;if(!d)return null;return d.resources??={tab:'profile',container:'equipment',questState:'active',query:'',index:0,cursors:[''],next:'',rows:[],loading:false,error:null,selected:null};}
  function reset(){mailbox.reset();vault.reset();capacity.reset();gold.reset();points.reset();level.reset();profession.reset();expertJob.reset();cancel('character-resource-list');cancel('character-resource-detail');quests.reset();inventory.reset();}
  function fresh(){const m=model();if(!m)return;m.index=0;m.cursors=[''];m.next='';load();}
  function load(){
    const owner=state.detail,m=model();if(!owner||!m||m.tab==='profile')return;
    if(m.tab==='vault'){vault.open(owner.data.character);return;}
    if(m.tab==='gold'){gold.open(owner.data.character);return;}
    if(m.tab==='points'){points.open(owner.data.character);return;}
    if(m.tab==='level'){level.open(owner.data.character);return;}
    if(m.tab==='profession'){profession.open(owner.data.character);return;}
    if(m.tab==='expert-job'){expertJob.open(owner.data.character);return;}
    if(m.tab==='capacity'){capacity.open(owner.data.character);return;}
    cancel('character-resource-list');cancel('character-resource-detail');mailbox.clearSelection();quests.clearSelection();inventory.clearSelection();m.loading=true;m.error=null;m.rows=[];m.next='';m.selected=null;m.observedAt='';renderDialog();
    const q=new URLSearchParams({limit:'20'});if(m.cursors[m.index])q.set('cursor',m.cursors[m.index]);
    let path='v1/characters/'+encodeURIComponent(owner.id)+'/';
    if(m.tab==='inventory'){q.set('container',m.container);path+='inventory';}
    if(m.tab==='mailbox'){path+='mailbox';for(const [key,value] of Object.entries(m.mailFilter||{}))if(value)q.set(key,value);}
    if(m.tab==='mailbox-log')path+='mailbox-operations';
    if(m.tab==='quest-log')path+='quest-operations';
    if(m.tab==='inventory-log')path+='inventory-operations';
    if(m.tab==='account-inventory-log')path='v1/accounts/'+encodeURIComponent(owner.data.character.accountId)+'/inventory-operations';
    if(m.tab==='quests'){q.set('state',m.questState);path+='quests';}
    if(m.tab==='catalog'){q.set('q',m.query);path='v1/quests';}
    request('character-resource-list',path+'?'+q,body=>{
      if(state.detail!==owner||model()!==m)return;
      m.rows=Array.isArray(body.data)?body.data:body.data.items||body.data.quests||[];
      m.next=body.page?.nextCursor||'';m.observedAt=body.observedAt;m.loading=false;renderDialog();
    },e=>{if(state.detail!==owner||model()!==m)return;m.loading=false;m.error=e;renderDialog();});
  }
  function detail(id,template=false){
    const owner=state.detail,m=model();if(!owner||!m)return;
    const selected={loading:true,error:null,data:null,template};m.selected=selected;renderDialog();
    let path='v1/characters/'+encodeURIComponent(owner.id)+'/';
    if(template)path='v1/items/'+encodeURIComponent(id);
    else if(m.tab==='inventory')path+='inventory/'+encodeURIComponent(m.container)+'/'+encodeURIComponent(id);
    else if(m.tab==='mailbox')path+='mailbox/'+encodeURIComponent(id);
    else path='v1/quests/'+encodeURIComponent(id);
    request('character-resource-detail',path,body=>{if(state.detail!==owner||m.selected!==selected)return;selected.loading=false;selected.data=body.data;renderDialog();},e=>{if(state.detail!==owner||m.selected!==selected)return;selected.loading=false;selected.error=e;renderDialog();});
  }
  function tabs(){const m=model();if(!m)return '';return '<div class="resource-tabs" role="tablist" aria-label="角色资料页签">'+[['profile','基本资料'],['gold','金币与流水'],['points','SP / TP'],['level','等级与成长'],['profession','转职与觉醒'],['expert-job','副职业'],['capacity','容量管理'],['vault','账号金库'],['inventory','背包与仓库'],['mailbox','邮箱'],['mailbox-log','邮件删除记录'],['quests','游戏任务'],['catalog','任务库'],['quest-log','任务操作记录'],['inventory-log','物品操作记录'],['account-inventory-log','账号物品记录']].map(([id,label])=>'<button type="button" role="tab" aria-selected="'+(m.tab===id)+'" class="button '+(m.tab===id?'primary':'')+'" data-resource-tab="'+id+'">'+label+'</button>').join('')+'</div>';}
  function selection(m){
    const selected=m.selected;if(!selected)return '';if(selected.loading)return '<section class="resource-detail" aria-live="polite">正在读取详情…</section>';
    if(selected.error)return '<section class="resource-detail">'+error(selected.error)+'</section>';
    const d=selected.data;let html='';
    if(selected.template)html=renderItemDetail(d);
    else if(m.tab==='inventory'){
      const creature=d.container==='pets'||(d.container==='equipped'&&d.slot===26);
      const artifactFields=d.petArtifact?field('宠物装备槽',{red:'红色',blue:'蓝色',green:'绿色'}[d.petArtifact.color])+field('关联记录','已匹配'):'';
      const petFields=d.pet?field('宠物名称',d.pet.name)+field('宠物等级',d.pet.level)+field('宠物经验',d.pet.experience)+field('饱食度',d.pet.satiety)+field('宠物编号',d.pet.creatureKey||d.pet.petKey)+field('出战状态',d.pet.equippedKey===d.pet.petKey?'出战中':'未出战'):'';
      const attributes=Object.entries(d.attributes||{}).filter(([k,v])=>!creature||(v!=null&&['enchant_id','enchant_card_id'].includes(k)));
      html='<h3>'+escape(d.name||'当前 PVF 未收录')+' <small class="mono">'+escape(d.itemId)+'</small></h3><dl class="detail-grid">'+petFields+artifactFields+field('槽位',d.slot)+field('数量',d.count)+field('绑定',d.bound?'已绑定':'未绑定')+field('到期时间',stamp(d.expiresAt))+attributes.map(([k,v])=>field(labels[k]||k,v)).join('')+'</dl>'+((d.petLinkError||d.petArtifactLinkError)?'<p role="alert">'+escape(d.petLinkError||d.petArtifactLinkError)+'</p>':'')+(d.definitionKnown?'<button type="button" class="button" data-resource-template="'+escape(d.itemId)+'">查看物品定义</button>':'');
    }
    else if(m.tab==='mailbox')html='<h3>'+escape(d.title)+'</h3><p class="resource-text">'+escape(d.body||'')+'</p><dl class="detail-grid">'+field('发件人',d.sender)+field('金币',d.gold)+field('附件领取',d.claimed?'已领取':'未领取')+'</dl><h4>附件</h4>'+(d.attachments.length?d.attachments.map(a=>'<p>'+escape(a.name||a.itemId)+' × '+escape(a.count)+'</p>').join(''):'<p>无附件</p>');
    else if(m.tab==='mailbox-log')html='<h3>删除邮件</h3><p>'+escape(d.reason)+'</p><p>'+escape(d.actor)+' · '+escape(stamp(d.createdAt))+'</p><small class="mono">'+escape(d.requestId)+'</small>'+mailboxChangesView(d.changes);
    else if(m.tab==='inventory-log'||m.tab==='account-inventory-log')html='<h3>'+escape(d.action==='delete'?'删除物品':'配置物品')+'</h3><p>'+escape(d.reason)+'</p><p>'+escape(d.actor)+' · '+escape(stamp(d.createdAt))+'</p><small class="mono">'+escape(d.requestId)+'</small>'+inventoryChangesView(d.changes);
    else if(m.tab==='quest-log')html='<h3>'+escape(questActions[d.action])+'</h3><p>'+escape(d.reason)+'</p><p>'+escape(d.actor)+' · '+escape(stamp(d.createdAt))+'</p><small class="mono">'+escape(d.requestId)+'</small>'+questChangesView(d.changes);
    else html='<h3>'+escape(d.name||d.questId)+'</h3><dl class="detail-grid">'+field('任务编号',d.questId)+field('等级',d.levelMin+'–'+d.levelMax)+field('分类',questGrade(d.grade))+field('职业条件',d.job||'PVF 未提供')+'</dl>'+[['接取说明',d.dependMessage],['任务目标',d.conditionMessage],['完成说明',d.solveMessage]].map(([label,text])=>'<h4>'+label+'</h4><p class="resource-text">'+escape(text||'PVF 未提供')+'</p>').join('')+'<h4>前置任务</h4><p>'+escape(d.prerequisites.map(g=>g.join(' / ')).join('；')||'无')+'</p><h4>奖励</h4><p>'+escape(d.rewardType||'PVF 未提供')+'</p>'+[...(d.rewards||[]).map(a=>({...a,choice:false})),...(d.selectionRewards||[]).map(a=>({...a,choice:true}))].map(a=>'<p>'+(a.choice?'可选：':'')+escape(a.itemId)+' × '+escape(a.count)+(a.jobFiltered?' · 职业 '+escape(a.job)+' / 成长 '+escape(a.growType):'')+'</p>').join('')+(d.rewardParseError?'<p>'+escape(d.rewardParseError)+'</p>':'');
    return '<section class="resource-detail" aria-label="所选记录详情">'+html+'</section>';
  }
  function view(){
    const m=model();if(!m||m.tab==='profile')return '';if(m.tab==='vault')return vault.view();if(m.tab==='gold')return gold.view();if(m.tab==='points')return points.view();if(m.tab==='level')return level.view();if(m.tab==='profession')return profession.view();if(m.tab==='expert-job')return expertJob.view();if(m.tab==='capacity')return capacity.view();
    let controls='';
    if(m.tab==='mailbox'){const f=m.mailDraft||{};controls=`<form id="resource-mail-filter"><label>邮件标题或编号<input name="q" maxlength="128" value="${escape(f.q||'')}"></label><label>邮件发件人<input name="sender" maxlength="64" value="${escape(f.sender||'')}"></label><label>附件领取状态<select name="claimed" aria-label="附件领取状态">${[['all','全部'],['claimed','已领取'],['unclaimed','未领取']].map(([v,t])=>`<option value="${v}" ${(f.claimed||'all')===v?'selected':''}>${t}</option>`).join('')}</select></label><label>起始时间<input type="datetime-local" name="from" value="${escape(f.from||'')}"></label><label>截止时间（不含）<input type="datetime-local" name="to" value="${escape(f.to||'')}"></label><div class="actions"><button type="submit" class="button primary" ${m.loading?'disabled':''}>筛选邮件</button>${button('清除邮件筛选','mail-clear',m.loading)}</div></form>`;}
    if(m.tab==='inventory')controls='<label>物品容器<select data-resource-select="container">'+containers.map(([id,label])=>'<option value="'+id+'" '+(m.container===id?'selected':'')+'>'+label+'</option>').join('')+'</select></label>';
    if(m.tab==='quests')controls='<label>游戏任务状态<select data-resource-select="questState">'+[['active','进行中'],['completed','已完成'],['all','全部记录']].map(([v,t])=>'<option value="'+v+'" '+(m.questState===v?'selected':'')+'>'+t+'</option>').join('')+'</select></label>';
    if(m.tab==='catalog')controls='<form id="resource-quest-search"><label>任务名称或 ID<input name="query" maxlength="128" value="'+escape(m.query)+'" '+(m.loading?'disabled':'')+'></label><button type="submit" class="button" '+(m.loading?'disabled':'')+'>查询任务库</button></form>';
    let content=m.loading?'<p role="status">正在读取…</p>':m.error?error(m.error):!m.rows.length?'<p class="empty-state">暂无记录</p>':'';
    if(!m.loading&&!m.error&&m.rows.length){
      let headings,rows;
      if(m.tab==='inventory'){headings=['选择','槽位','物品','数量','绑定','操作'];rows=m.rows.map(r=>'<tr><td>'+inventory.checkbox(r)+'</td><td>'+r.slot+'</td><td>'+escape(r.name||'当前 PVF 未收录')+'<small class="mono">'+escape(r.itemId)+'</small></td><td>'+escape(r.count)+'</td><td>'+(r.bound?'已绑定':'未绑定')+'</td><td><button type="button" class="button link" data-resource-detail="'+r.slot+'">实例详情</button>'+(inventory.configurable(m.container,r.slot)?'<button type="button" class="button link" data-inventory-edit="'+r.slot+'" aria-label="编辑槽位 '+r.slot+'">编辑</button>':'')+'</td></tr>');}
      else if(m.tab==='mailbox'){headings=['选择','邮件','发件人','附件领取','时间','操作'];rows=m.rows.map(r=>'<tr><td>'+mailbox.checkbox(r)+'</td><td>'+escape(r.title)+'<small class="mono">'+escape(r.mailId)+'</small></td><td>'+escape(r.sender)+'</td><td>'+(r.claimed?'已领取':'未领取')+'</td><td>'+escape(stamp(r.createdAt))+'</td><td><button type="button" class="button link" data-resource-detail="'+escape(r.mailId)+'">查看邮件</button><button type="button" class="button link" data-mail-delete="'+escape(r.mailId)+'">删除邮件</button></td></tr>');}
      else if(m.tab==='mailbox-log'){headings=['时间','邮件数','丢弃未领取资产','原因','详情'];rows=m.rows.map((r,i)=>'<tr><td>'+escape(stamp(r.createdAt))+'</td><td>'+r.changes.length+'</td><td>'+r.changes.filter(c=>c.discardedAssets).length+' 封</td><td>'+escape(r.reason)+'</td><td><button type="button" class="button link" data-mail-receipt="'+i+'">查看邮件回执</button></td></tr>');}
      else if(m.tab==='inventory-log'||m.tab==='account-inventory-log'){headings=['时间','操作','物品格数','原因','详情'];rows=m.rows.map((r,i)=>'<tr><td>'+escape(stamp(r.createdAt))+'</td><td>'+escape(r.action==='delete'?'删除物品':'配置物品')+'</td><td>'+r.changes.length+'</td><td>'+escape(r.reason)+'</td><td><button type="button" class="button link" data-inventory-receipt="'+i+'">查看物品回执</button></td></tr>');}
      else if(m.tab==='quest-log'){headings=['时间','操作','变化','原因','详情'];rows=m.rows.map((r,i)=>'<tr><td>'+escape(stamp(r.createdAt))+'</td><td>'+escape(questActions[r.action])+'</td><td>'+r.changes.filter(c=>c.changed).length+' 项</td><td>'+escape(r.reason)+'</td><td><button type="button" class="button link" data-quest-receipt="'+i+'">查看回执</button></td></tr>');}
      else{headings=['选择','任务','分类','等级','进度 / 状态','操作'];rows=m.rows.map(r=>'<tr><td>'+quests.checkbox(r)+'</td><td>'+escape(r.name||(r.definitionKnown===false?'当前 PVF 未收录':'未提供任务名称'))+'<small class="mono">'+escape(r.questId)+'</small></td><td>'+escape(questGrade(r.grade))+'</td><td>'+(r.levelMin??'—')+'–'+(r.levelMax??'—')+'</td><td>'+escape(r.progress??'—')+' / '+escape(questStatus(r.status))+'</td><td><button type="button" class="button link" data-resource-detail="'+escape(r.questId)+'" '+(r.definitionKnown===false?'disabled':'')+'>任务详情</button></td></tr>');}
      content='<div class="table-wrap"><table><thead><tr>'+headings.map(h=>'<th>'+h+'</th>').join('')+'</tr></thead><tbody>'+rows.join('')+'</tbody></table></div>';
    }
    return '<section class="character-resources" role="tabpanel"><div class="resource-controls">'+controls+button('刷新记录','refresh',m.loading)+'</div>'+content+'<div class="table-footer"><span>第 '+(m.index+1)+' 页'+(m.observedAt?' · 读取于 '+escape(stamp(m.observedAt)):'')+'</span><div class="actions">'+button('上一页','prev',m.loading||m.index===0)+button('下一页','next',m.loading||!m.next)+'</div></div>'+selection(m)+(m.tab==='mailbox'?mailbox.view():'')+(m.tab==='inventory'?inventory.view():'')+(['quests','catalog'].includes(m.tab)?quests.view():'')+'</section>';
  }
  document.addEventListener('click',e=>{
    const b=e.target.closest('button');if(!b||!b.closest('#dialog'))return;const m=model();if(!m)return;
    if(b.dataset.resourceTab){mailbox.reset();vault.reset();capacity.reset();gold.reset();points.reset();level.reset();profession.reset();expertJob.reset();cancel('character-resource-list');cancel('character-resource-detail');mailbox.clearSelection();quests.clearSelection();inventory.clearSelection();m.tab=b.dataset.resourceTab;m.selected=null;fresh();renderDialog();}
    if(b.dataset.mailReceipt!==undefined){m.selected={data:m.rows[Number(b.dataset.mailReceipt)]};renderDialog();}
    if(b.dataset.inventoryReceipt!==undefined){m.selected={data:m.rows[Number(b.dataset.inventoryReceipt)]};renderDialog();}
    if(b.dataset.questReceipt!==undefined){m.selected={data:m.rows[Number(b.dataset.questReceipt)]};renderDialog();}
    if(b.dataset.resourceDetail!==undefined)detail(b.dataset.resourceDetail);
    if(b.dataset.resourceTemplate)detail(b.dataset.resourceTemplate,true);
    if(b.dataset.resourceAction==='refresh')fresh();
    if(b.dataset.resourceAction==='mail-clear'){m.mailDraft={};m.mailFilter={};fresh();}
    if(b.dataset.resourceAction==='prev'&&m.index>0){m.index--;load();}
    if(b.dataset.resourceAction==='next'&&m.next){m.cursors=m.cursors.slice(0,m.index+1);m.cursors.push(m.next);m.index++;load();}
  });
  document.addEventListener('change',e=>{if(e.target.dataset.resourceSelect){const m=model();if(m){m[e.target.dataset.resourceSelect]=e.target.value;fresh();}}});
  document.addEventListener('input',e=>{if(e.target.closest('#resource-quest-search')){const m=model();if(m)m.query=e.target.value;}});
  const mailDraft=e=>{if(e.target.closest('#resource-mail-filter')){const m=model();if(m)(m.mailDraft??={})[e.target.name]=e.target.value;}};
  document.addEventListener('input',mailDraft);document.addEventListener('change',mailDraft);
  document.addEventListener('submit',e=>{if(e.target.id==='resource-mail-filter'){e.preventDefault();const m=model();if(!m)return;const f=Object.fromEntries(new FormData(e.target));try{for(const key of ['from','to']){if(f[key])f[key]=new Date(f[key]).toISOString();else delete f[key];}m.mailFilter=f;fresh();}catch{m.error=Error('请选择有效的起止时间。');renderDialog();}}});
  document.addEventListener('submit',e=>{if(e.target.id==='resource-quest-search'){e.preventDefault();fresh();}});
  return {tabs,view,reset,isProfile:()=>model()?.tab==='profile'};
}
