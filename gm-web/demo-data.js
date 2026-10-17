// Synthetic fixtures for visual and interaction review. No player data or API calls.
export const accounts = [
  {id:'demo-1001',name:'林间来信',login:'forest_mail',status:'在线',seen:'刚刚',characters:[{id:'c101',name:'风与旅人',job:'剑神',level:90,online:true},{id:'c102',name:'晚风来信',job:'念帝',level:86,online:false}]},
  {id:'demo-1002',name:'月光航线',login:'moonlight',status:'离线',seen:'12 分钟前',characters:[{id:'c201',name:'月下航行',job:'极诣·鬼泣',level:90,online:false}]},
  {id:'demo-1003',name:'薄荷汽水',login:'mint_soda',status:'在线',seen:'刚刚',characters:[{id:'c301',name:'薄荷冰',job:'魔皇',level:90,online:true},{id:'c302',name:'汽水泡泡',job:'炽天使',level:75,online:false}]},
  {id:'demo-1004',name:'山海之间',login:'mountain',status:'离线',seen:'1 小时前',characters:[{id:'c401',name:'山海拾光',job:'天帝',level:90,online:false}]},
  {id:'demo-1005',name:'暮色收藏家',login:'dusk',status:'离线',seen:'2 小时前',characters:[]},
  {id:'demo-1006',name:'晴空漫步',login:'clear_sky',status:'在线',seen:'刚刚',characters:[{id:'c601',name:'晴空',job:'风神',level:82,online:true}]},
  {id:'demo-1007',name:'雨声备忘录',login:'rain_note',status:'离线',seen:'昨天',characters:[{id:'c701',name:'听雨',job:'不灭战神',level:88,online:false}]},
  {id:'demo-1008',name:'星河向晚',login:'star_river',status:'离线',seen:'昨天',characters:[{id:'c801',name:'星河',job:'古灵精怪',level:90,online:false}]}
];
export const items = [
  {id:490009735,name:'天空自选礼盒',type:'消耗品',rarity:'稀有',level:1,icon:'gift',description:'时装礼盒'},
  {id:10010001,name:'远行者的长剑',type:'武器',rarity:'史诗',level:90,icon:'sword',description:'长剑'},
  {id:10010002,name:'暮光轻甲上衣',type:'防具',rarity:'传说',level:85,icon:'shield',description:'轻甲上衣'},
  {id:10010003,name:'冒险补给箱',type:'消耗品',rarity:'稀有',level:1,icon:'box',description:'补给箱'},
  {id:10010004,name:'清晨之露',type:'消耗品',rarity:'普通',level:1,icon:'spark',description:'恢复道具'},
  {id:10010005,name:'苍穹结晶',type:'材料',rarity:'传说',level:1,icon:'diamond',description:'制作材料'},
  {id:10010006,name:'星芒指环',type:'首饰',rarity:'史诗',level:90,icon:'diamond',description:'戒指'},
  {id:10010007,name:'公会纪念徽章',type:'材料',rarity:'稀有',level:1,icon:'shield',description:'公会材料'}
];
export const initialTasks = [
  {id:'DEMO-0003',title:'新角色欢迎礼',kind:'福利规则',status:'待确认',target:'新建角色',time:'今天 14:30',detail:'规则状态：停用。范围：每角色一次。'},
  {id:'DEMO-0002',title:'周末冒险补给',kind:'邮件草稿',status:'草稿',target:'3 个角色',time:'今天 11:20',detail:'3 个收件人，1 类附件。'},
  {id:'DEMO-0001',title:'物品目录导入',kind:'目录任务',status:'已完成',target:'物品目录',time:'今天 09:00',detail:'目录已更新，共 149,574 个物品。'}
];
