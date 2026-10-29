/* Override review, theme tokens and Docker-retained history UI contracts. */
async (page) => {
 const errors=[];page.on('pageerror',error=>errors.push(error.message));
 await page.addInitScript(()=>{window.EventSource=class extends EventTarget{constructor(){super();this.readyState=1;}close(){}};});
 const id='a'.repeat(64),fp='f'.repeat(64),candidate='c'.repeat(64);
 const metrics={cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null};
 const container={...metrics,id,name:'app-1',service:'app',image:'fixture/app',state:'running',health:'none',terminalAvailable:true};
 const project={...metrics,id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'none',containers:[container]};
 const base='services:\n  app:\n    image: fixture/app\n    environment:\n      VALUE: old\n';
 const merged=base.replace('VALUE: old','VALUE: new\n      ADDED: kept');
 const override='services:\n  app:\n    environment: {VALUE: new, ADDED: kept}\n';
 let saved=false,applies=0,previews=0,history=0;const historyBounds=[];
 const view=reveal=>({available:true,state:'ready',directory:'/srv/sample',fingerprint:fp,composeFiles:saved?['compose.yaml','compose.override.yaml']:['compose.yaml'],profiles:[],availableProfiles:['debug'],interpolationFiles:['.env'],variables:[],files:[{path:'compose.yaml',compose:true,content:reveal?base:''},{path:'.env',compose:false,content:reveal?'TOKEN=private\n':''},...(saved?[{path:'compose.override.yaml',compose:true,content:reveal?override:''}]:[])]});
 await page.route('**/api/**',async route=>{
  const request=route.request(),url=new URL(request.url()),path=url.pathname;
  if(path==='/api/bootstrap')return route.fulfill({json:{needsSetup:false,authenticated:true,username:'owner',csrfToken:'fixture-csrf'}});
  if(path==='/api/jobs')return route.fulfill({json:[]});
  if(path==='/api/projects')return route.fulfill({json:{projects:[project],collectedAt:new Date().toISOString()}});
  if(path.endsWith('/configuration/read'))return route.fulfill({json:view(request.postDataJSON()?.reveal)});
  if(path.endsWith('/configuration/override-preview')){
   const input=request.postDataJSON();previews++;if(input.fingerprint!==fp||input.source.kind!=='paste'||input.source.yaml!==override)throw Error('override source/snapshot changed');
   return route.fulfill({json:{action:'add',path:'compose.override.yaml',source:{kind:'paste',yaml:override},fingerprint:fp,candidateFingerprint:candidate,before:base,after:merged,composeFiles:['compose.yaml','compose.override.yaml']}});
  }
  if(path.endsWith('/configuration/override-apply')){
   const input=request.postDataJSON();if(Object.keys(input).sort().join(',')!=='action,candidateFingerprint,fingerprint,path,source')throw Error('apply sent display-only fields');if(input.source.kind!=='paste'||input.source.yaml!==override||input.candidateFingerprint!==candidate)throw Error('apply did not use frozen reviewed bytes');applies++;saved=true;return route.fulfill({json:view(true)});
  }
  if(path.endsWith('/logs/export'))return route.fulfill({json:{available:true,directory:'/srv/sample/logs'}});
  if(path.endsWith('/logs/history')){history++;historyBounds.push(url.searchParams.get('until'));if(url.searchParams.get('cursor'))return route.fulfill({json:{lines:[{stream:'stdout',text:'same timestamp: repeated',lineEnd:true}],until:'2026-10-28T12:00:00Z',retention:'Docker only'}});return route.fulfill({json:{lines:[{stream:'stdout',text:'older record',lineEnd:true},{stream:'stdout',text:'same timestamp: repeated',lineEnd:true}],next:'opaque-next',until:'2026-10-28T12:00:00Z',retention:'Docker only'}});}
  if(path.includes('/api/containers/'))return route.fulfill({json:{id,ports:[],mounts:[],networks:[],environment:[]}});
  if(path.endsWith('/schedule'))return route.fulfill({json:{enabled:false,time:'03:00',timezone:'Europe/Lisbon',eligible:true}});
  if(path==='/api/self-update')return route.fulfill({json:{automatic:false,status:'not_checked'}});
  if(path==='/api/managed/settings')return route.fulfill({json:{projectsBase:'/srv'}});
  return route.fulfill({json:{}});
 });
 await page.emulateMedia({reducedMotion:'reduce'});await page.goto('http://127.0.0.1:5173');
 await page.getByRole('button',{name:/Managed Compose running sample/}).click();
 const drawer=page.getByRole('dialog',{name:'sample',exact:true});
 await drawer.getByText('Compose and environment files',{exact:true}).click();
 await drawer.getByLabel('compose.yaml',{exact:true}).waitFor();
 await drawer.getByRole('button',{name:'Add override',exact:true}).click();
 let overrideDrawer=page.getByRole('dialog',{name:'Add override',exact:true});
 await overrideDrawer.getByLabel('Paste YAML',{exact:true}).check();
 await overrideDrawer.getByLabel('Compose YAML',{exact:true}).fill(override);
 await overrideDrawer.getByRole('button',{name:'Read source',exact:true}).click();
 await overrideDrawer.getByLabel('Compose changes',{exact:true}).waitFor();
 if(!await overrideDrawer.locator('[data-change="modified"]').count()||!await overrideDrawer.locator('[data-change="added"]').count())throw Error('changed Compose lines have no markers');
 const contrast=await overrideDrawer.getByLabel('Compose changes',{exact:true}).evaluate(pre=>{
  const rgb=value=>value.match(/[\d.]+/g).map(Number),base=rgb(getComputedStyle(pre).backgroundColor);
  const lum=c=>c.map(v=>v/255).map(v=>v<=.04045?v/12.92:Math.pow((v+.055)/1.055,2.4)).reduce((sum,v,i)=>sum+v*[.2126,.7152,.0722][i],0);
  return [...pre.querySelectorAll('[data-change]')].map(el=>{const style=getComputedStyle(el),fg=rgb(style.color),raw=rgb(style.backgroundColor),a=raw[3]??1,bg=raw.slice(0,3).map((v,i)=>v*a+base[i]*(1-a));const x=lum(fg),y=lum(bg);return (Math.max(x,y)+.05)/(Math.min(x,y)+.05);});
 });if(contrast.some(value=>value<4.5))throw Error('Compose diff text contrast is below 4.5:1');
 await page.keyboard.press('Escape');
 const discard=page.getByRole('dialog',{name:'Discard override draft?',exact:true});await discard.getByRole('button',{name:'Keep editing',exact:true}).click();
 await overrideDrawer.getByLabel('Compose changes',{exact:true}).waitFor();
 await overrideDrawer.getByRole('button',{name:'Show final Compose',exact:true}).click();
 if((await overrideDrawer.getByLabel('Final effective Compose').innerText()).trim()!==merged.trim())throw Error('final Compose differs from preview');
 await overrideDrawer.getByRole('button',{name:'Cancel',exact:true}).click();
 if(applies!==0)throw Error('cancel wrote override');
 await drawer.getByRole('button',{name:'Add override',exact:true}).click();
 overrideDrawer=page.getByRole('dialog',{name:'Add override',exact:true});
 await overrideDrawer.getByLabel('Paste YAML',{exact:true}).check();await overrideDrawer.getByLabel('Compose YAML',{exact:true}).fill(override);
 await overrideDrawer.getByRole('button',{name:'Read source',exact:true}).click();await overrideDrawer.getByLabel('Compose changes',{exact:true}).waitFor();
 for(const width of [1440,768,390,320]){
  await page.setViewportSize({width,height:1000});
  const overflow=await overrideDrawer.evaluate(el=>el.scrollWidth>el.clientWidth+1);if(overflow)throw Error('override drawer overflows at '+width);
 }
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 const dialogs=await page.getByRole('dialog',{name:'Add override',exact:true}).count();if(dialogs!==1)throw Error('duplicate override modal');
 await page.screenshot({path:'output/playwright/compose-override-mobile.png'});
 await overrideDrawer.getByRole('button',{name:'Add override',exact:true}).click();
 await drawer.getByLabel('compose.override.yaml',{exact:true}).waitFor();
 if(applies!==1||previews!==2||await drawer.getByLabel('compose.yaml',{exact:true}).inputValue()!==base)throw Error('override modified original or saved more than once');
 await drawer.getByRole('tab',{name:'Logs',exact:true}).click();
 await drawer.getByRole('button',{name:'Load history',exact:true}).click();
 await drawer.getByText('older record',{exact:false}).waitFor();
 await drawer.getByRole('button',{name:'Next page',exact:true}).click();
 await drawer.getByText('End of available history',{exact:true}).waitFor();
 const log=await drawer.getByLabel('app log output').innerText();if(!log.includes('same timestamp: repeated')||log.includes('older record')||history!==2)throw Error('history pagination altered records or did not bound the page');
 await drawer.getByRole('button',{name:'Previous page',exact:true}).click();
 await drawer.getByText('older record',{exact:false}).waitFor();
 if(historyBounds[0]!==historyBounds[2])throw Error('previous page changed the frozen history interval');
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: override review/save/cancel preserves source, responsive diff markers and paged Docker history');
}
