import {$,escape} from './ui.js';
import {renderItemDetail} from './item-detail-view.js';
const containers=[['equipment','背包装备'],['consumables','消耗品'],['materials','材料'],['quest-items','任务物品'],['profession','副职业材料'],['quickbar','快捷栏'],['avatars','装扮'],['pets','宠物'],['pet-equipment','宠物装备'],['pet-consumables','宠物消耗品'],['medals','勋章与守护珠'],['equipped','身上穿戴'],['warehouse','个人仓库'],['account-cargo','账号金库'],['shared','账号晶块与灵魂']];
const labels={qualitySeed:'品级种子',reinforce:'强化等级',amplifyType:'增幅类型',amplifyValue:'增幅数值',forging:'锻造等级',durability:'当前耐久',max_durability:'最大耐久',seal:'封装状态',serial:'实例编号',enchant_id:'附魔 ID',enchant_card_id:'附魔卡 ID',avatar_option_value:'装扮选项',emblem_0:'徽章 1',emblem_1:'徽章 2',emblem_2:'徽章 3'};
const stamp=v=>v?new Date(v).toLocaleString('zh-CN',{hour12:false}):'—';
const button=(label,action,disabled=false)=>'<button type="button" class="button" data-resource-action="'+action+'" '+(disabled?'disabled':'')+'>'+escape(label)+'</button>';
const field=(name,value)=>'<div><dt>'+escape(name)+'</dt><dd>'+escape(value??'未知')+'</dd></div>';
const error=e=>'<div class="hint-box" role="alert">'+escape(e.message)+(e.requestId?'<p class="mono">请求编号：'+escape(e.requestId)+'</p>':'')+'</div>';

export function createCharacterResources({request,cancel,state,renderDialog}){
  function model(){const d=state.detail;if(!d)return null;return d.resources??={tab:'profile',container:'equipment',questState:'active',query:'',index:0,cursors:[''],next:'',rows:[],loading:false,error:null,selected:null};}
  function reset(){cancel('character-resource-list');cancel('character-resource-detail');}
  function fresh(){const m=model();if(!m)return;m.index=0;m.cursors=[''];m.next='';load();}
  function load(){
    const owner=state.detail,m=model();if(!owner||!m||m.tab==='profile')return;
    reset();m.loading=true;m.error=null;m.rows=[];m.next='';m.selected=null;m.observedAt='';renderDialog();
    const q=new URLSearchParams({limit:'20'});if(m.cursors[m.index])q.set('cursor',m.cursors[m.index]);
    let path='v1/characters/'+encodeURIComponent(owner.id)+'/';
    if(m.tab==='inventory'){q.set('container',m.container);path+='inventory';}
    if(m.tab==='mailbox')path+='mailbox';
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
  function tabs(){const m=model();if(!m)return '';return '<div class="resource-tabs" role="tablist" aria-label="角色资料页签">'+[['profile','基本资料'],['inventory','背包与仓库'],['mailbox','邮箱'],['quests','游戏任务'],['catalog','任务库']].map(([id,label])=>'<button type="button" role="tab" aria-selected="'+(m.tab===id)+'" class="button '+(m.tab===id?'primary':'')+'" data-resource-tab="'+id+'">'+label+'</button>').join('')+'</div>';}
  function selection(m){
    const selected=m.selected;if(!selected)return '';if(selected.loading)return '<section class="resource-detail" aria-live="polite">正在读取详情…</section>';
    if(selected.error)return '<section class="resource-detail">'+error(selected.error)+'</section>';
    const d=selected.data;let html='';
    if(selected.template)html=renderItemDetail(d);
    else if(m.tab==='inventory')html='<h3>'+escape(d.name||'当前 PVF 未收录')+' <small class="mono">'+escape(d.itemId)+'</small></h3><dl class="detail-grid">'+field('槽位',d.slot)+field('数量',d.count)+field('绑定',d.bound?'已绑定':'未绑定')+field('到期时间',stamp(d.expiresAt))+Object.entries(d.attributes||{}).map(([k,v])=>field(labels[k]||k,v)).join('')+'</dl>'+(d.definitionKnown?'<button type="button" class="button" data-resource-template="'+escape(d.itemId)+'">查看物品定义</button>':'');
    else if(m.tab==='mailbox')html='<h3>'+escape(d.title)+'</h3><p class="resource-text">'+escape(d.body||'')+'</p><dl class="detail-grid">'+field('发件人',d.sender)+field('金币',d.gold)+field('附件领取',d.claimed?'已领取':'未领取')+'</dl><h4>附件</h4>'+(d.attachments.length?d.attachments.map(a=>'<p>'+escape(a.name||a.itemId)+' × '+escape(a.count)+'</p>').join(''):'<p>无附件</p>');
    else html='<h3>'+escape(d.name||d.questId)+'</h3><dl class="detail-grid">'+field('任务编号',d.questId)+field('等级',d.levelMin+'–'+d.levelMax)+field('分类',d.grade)+field('职业条件',d.job||'PVF 未提供')+'</dl>'+[['接取说明',d.dependMessage],['任务目标',d.conditionMessage],['完成说明',d.solveMessage]].map(([label,text])=>'<h4>'+label+'</h4><p class="resource-text">'+escape(text||'PVF 未提供')+'</p>').join('')+'<h4>前置任务</h4><p>'+escape(d.prerequisites.map(g=>g.join(' / ')).join('；')||'无')+'</p><h4>奖励</h4><p>'+escape(d.rewardType||'PVF 未提供')+'</p>'+[...(d.rewards||[]).map(a=>({...a,choice:false})),...(d.selectionRewards||[]).map(a=>({...a,choice:true}))].map(a=>'<p>'+(a.choice?'可选：':'')+escape(a.itemId)+' × '+escape(a.count)+(a.jobFiltered?' · 职业 '+escape(a.job)+' / 成长 '+escape(a.growType):'')+'</p>').join('')+(d.rewardParseError?'<p>'+escape(d.rewardParseError)+'</p>':'');
    return '<section class="resource-detail" aria-label="所选记录详情">'+html+'</section>';
  }
  function view(){
    const m=model();if(!m||m.tab==='profile')return '';
    let controls='';
    if(m.tab==='inventory')controls='<label>物品容器<select data-resource-select="container">'+containers.map(([id,label])=>'<option value="'+id+'" '+(m.container===id?'selected':'')+'>'+label+'</option>').join('')+'</select></label>';
    if(m.tab==='quests')controls='<label>游戏任务状态<select data-resource-select="questState">'+[['active','进行中'],['completed','已完成'],['all','全部记录']].map(([v,t])=>'<option value="'+v+'" '+(m.questState===v?'selected':'')+'>'+t+'</option>').join('')+'</select></label>';
    if(m.tab==='catalog')controls='<form id="resource-quest-search"><label>任务名称或 ID<input name="query" maxlength="128" value="'+escape(m.query)+'" '+(m.loading?'disabled':'')+'></label><button type="submit" class="button" '+(m.loading?'disabled':'')+'>查询任务库</button></form>';
    let content=m.loading?'<p role="status">正在读取…</p>':m.error?error(m.error):!m.rows.length?'<p class="empty-state">暂无记录</p>':'';
    if(!m.loading&&!m.error&&m.rows.length){
      let headings,rows;
      if(m.tab==='inventory'){headings=['槽位','物品','数量','绑定','操作'];rows=m.rows.map(r=>'<tr><td>'+r.slot+'</td><td>'+escape(r.name||'当前 PVF 未收录')+'<small class="mono">'+escape(r.itemId)+'</small></td><td>'+escape(r.count)+'</td><td>'+(r.bound?'已绑定':'未绑定')+'</td><td><button type="button" class="button link" data-resource-detail="'+r.slot+'">实例详情</button></td></tr>');}
      else if(m.tab==='mailbox'){headings=['邮件','发件人','附件领取','时间','操作'];rows=m.rows.map(r=>'<tr><td>'+escape(r.title)+'<small class="mono">'+escape(r.mailId)+'</small></td><td>'+escape(r.sender)+'</td><td>'+(r.claimed?'已领取':'未领取')+'</td><td>'+escape(stamp(r.createdAt))+'</td><td><button type="button" class="button link" data-resource-detail="'+escape(r.mailId)+'">查看邮件</button></td></tr>');}
      else{headings=['任务','分类','等级','进度 / 状态','操作'];rows=m.rows.map(r=>'<tr><td>'+escape(r.name||(r.definitionKnown===false?'当前 PVF 未收录':'未提供任务名称'))+'<small class="mono">'+escape(r.questId)+'</small></td><td>'+escape(r.grade||'—')+'</td><td>'+(r.levelMin??'—')+'–'+(r.levelMax??'—')+'</td><td>'+escape(r.progress??'—')+' / '+escape(r.status||'—')+'</td><td><button type="button" class="button link" data-resource-detail="'+escape(r.questId)+'" '+(r.definitionKnown===false?'disabled':'')+'>任务详情</button></td></tr>');}
      content='<div class="table-wrap"><table><thead><tr>'+headings.map(h=>'<th>'+h+'</th>').join('')+'</tr></thead><tbody>'+rows.join('')+'</tbody></table></div>';
    }
    return '<section class="character-resources" role="tabpanel"><div class="resource-controls">'+controls+button('刷新记录','refresh',m.loading)+'</div>'+content+'<div class="table-footer"><span>第 '+(m.index+1)+' 页'+(m.observedAt?' · 读取于 '+escape(stamp(m.observedAt)):'')+'</span><div class="actions">'+button('上一页','prev',m.loading||m.index===0)+button('下一页','next',m.loading||!m.next)+'</div></div>'+selection(m)+'</section>';
  }
  document.addEventListener('click',e=>{
    const b=e.target.closest('button');if(!b||!b.closest('#dialog'))return;const m=model();if(!m)return;
    if(b.dataset.resourceTab){reset();m.tab=b.dataset.resourceTab;m.selected=null;fresh();renderDialog();}
    if(b.dataset.resourceDetail!==undefined)detail(b.dataset.resourceDetail);
    if(b.dataset.resourceTemplate)detail(b.dataset.resourceTemplate,true);
    if(b.dataset.resourceAction==='refresh')fresh();
    if(b.dataset.resourceAction==='prev'&&m.index>0){m.index--;load();}
    if(b.dataset.resourceAction==='next'&&m.next){m.cursors=m.cursors.slice(0,m.index+1);m.cursors.push(m.next);m.index++;load();}
  });
  document.addEventListener('change',e=>{if(e.target.dataset.resourceSelect){const m=model();if(m){m[e.target.dataset.resourceSelect]=e.target.value;fresh();}}});
  document.addEventListener('input',e=>{if(e.target.closest('#resource-quest-search')){const m=model();if(m)m.query=e.target.value;}});
  document.addEventListener('submit',e=>{if(e.target.id==='resource-quest-search'){e.preventDefault();fresh();}});
  return {tabs,view,reset,isProfile:()=>model()?.tab==='profile'};
}
