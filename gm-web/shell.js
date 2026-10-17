import {$,icon} from './ui.js';

export const nav=[['overview','概览','grid'],['players','账号与角色','users'],['items','物品目录','box'],['mail','邮件与福利','mail'],['tasks','任务记录','list'],['gateway','网关与更新','server'],['settings','工作台设置','settings']];
const mobile=matchMedia('(max-width:760px)');
const collapseKey='soft-gm-console.sidebar-collapsed';
let collapsed=false;
try{collapsed=localStorage.getItem(collapseKey)==='true';}catch{}

export function setNavigation(open){
  const shown=mobile.matches&&!!open;
  document.body.classList.toggle('nav-open',shown);
  document.body.classList.toggle('sidebar-collapsed',!mobile.matches&&collapsed);
  $('#sidebar').inert=mobile.matches&&!shown;
  $('.shell-content').inert=shown;
  $('#nav-scrim').hidden=!shown;
  const label=mobile.matches?(shown?'关闭导航':'打开导航'):(collapsed?'展开导航':'收起导航');
  $('#nav-toggle').setAttribute('aria-label',label);$('#nav-toggle').title=label;
  $('#nav-toggle').setAttribute('aria-expanded',String(mobile.matches?shown:!collapsed));
  if(shown)$('#navigation a[aria-current]')?.focus();
}
$('#navigation').innerHTML=nav.map(([route,title,glyph],index)=>`${index===1||index===5?'<hr class="nav-divider">':''}<a href="#${route}" title="${title}">${icon(glyph)}<span>${title}</span></a>`).join('');
$('#theme-open').innerHTML=icon('palette');$('#theme-open').setAttribute('aria-label','打开主题工作室');$('#theme-open').title='主题';
$('#nav-toggle').innerHTML=icon('menu');
$('#nav-toggle').addEventListener('click',()=>{
  if(mobile.matches)setNavigation(!document.body.classList.contains('nav-open'));
  else{collapsed=!collapsed;try{localStorage.setItem(collapseKey,String(collapsed));}catch{}setNavigation(false);}
});
$('#nav-scrim').addEventListener('click',()=>{setNavigation(false);$('#nav-toggle').focus();});
mobile.addEventListener('change',()=>setNavigation(false));
window.addEventListener('keydown',event=>{
  if(!document.body.classList.contains('nav-open'))return;
  if(event.key==='Escape'){setNavigation(false);$('#nav-toggle').focus();}
  if(event.key==='Tab'){const links=$('#sidebar').querySelectorAll('a'),first=links[0],last=links[links.length-1];if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}}
});
setNavigation(false);
