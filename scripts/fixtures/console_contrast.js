() => {
  const ctx=document.createElement('canvas').getContext('2d',{willReadFrequently:true});
  function rgba(color){ctx.clearRect(0,0,1,1);ctx.fillStyle=color;ctx.fillRect(0,0,1,1);const data=ctx.getImageData(0,0,1,1).data;return [data[0],data[1],data[2],data[3]/255];}
  function over(fg,bg){return [0,1,2].map(i=>fg[i]*fg[3]+bg[i]*(1-fg[3])).concat(1);}
  function parent(node){return node.parentElement||node.getRootNode().host;}
  function background(node){if(!node)return [255,255,255,1];const bg=rgba(getComputedStyle(node).backgroundColor);return bg[3]===1?bg:over(bg,background(parent(node)));}
  function luminance(rgb){const c=rgb.slice(0,3).map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4;});return c[0]*.2126+c[1]*.7152+c[2]*.0722;}
  const studio=document.querySelector('#cpamp-theme-studio-root').shadowRoot;
  const nodes=[...document.querySelectorAll('#main h1,#main th,#main td,#main .hint,#main .badge,#navigation a[aria-current],#main .button.primary'),...studio.querySelectorAll('.ts-button,.ts-head p,.ts-scope,.ts-section h3')];
  return nodes.map(node=>{const bg=background(node),fg=over(rgba(getComputedStyle(node).color),bg);const a=luminance(bg),b=luminance(fg);return {text:node.textContent.trim().slice(0,25),ratio:(Math.max(a,b)+.05)/(Math.min(a,b)+.05),selector:node.className};}).filter(r=>r.ratio<4.5);
}
