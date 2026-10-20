/* Drawer layout, grouped project navigation, live-log search and host export UI. */
async (originalPage) => {
  const page=await originalPage.context().newPage();
  try {
    const errors=[];page.on('pageerror',error=>errors.push(error.message));
    const ids=['a','b','c','d'].map(char=>char.repeat(64));
    const metrics={cpuPercent:1.2,memoryBytes:16*1024*1024,uptimeSeconds:90000,networkRxBytes:1024,networkTxBytes:2048};
    const container=(id,name)=>({...metrics,id,name:name+'-1',service:name,image:'fixture/app:latest',state:'running',health:'healthy',terminalAvailable:true});
    const projects=[{...metrics,id:'compose:single',name:'single',kind:'external-compose',state:'running',health:'healthy',containers:[container(ids[0],'api')]},{...metrics,id:'compose:multi',name:'multi',kind:'external-compose',state:'running',health:'healthy',containers:[container(ids[1],'web'),container(ids[2],'db')]},{...metrics,id:'container:'+ids[3],name:'standalone',kind:'standalone',state:'running',health:'healthy',containers:[{...container(ids[3],'standalone'),service:undefined}]}];
    const lines=Array.from({length:90},(_,index)=>`2026-10-20T11:37:${String(index%60).padStart(2,'0')} ${index===15||index===65?'ERROR connection failed':index===40?'literal [a+b] <script>untrusted</script>':'ready line '+index}`);
    let exports=0,failOnce=true;
    const saved=new Map();
    await page.addInitScript(logs=>{
      window.drawerStreams=[];
      class Events extends EventTarget {
        static OPEN=1;static CLOSED=2;
        constructor(url){super();this.url=url;this.readyState=1;window.drawerStreams.push(this);setTimeout(()=>{this.onopen?.();if(url.includes('/logs'))for(const text of logs)this.dispatchEvent(new MessageEvent('log',{data:JSON.stringify({stream:'stdout',text})}))},0)}
        close(){this.readyState=2}
      }
      class Socket {
        static OPEN=1;
        constructor(){this.readyState=1;setTimeout(()=>this.onopen?.(),0)}
        send(value){if(typeof value==='string'&&JSON.parse(value).type==='open')setTimeout(()=>this.onmessage?.({data:JSON.stringify({type:'ready'})}),0)}
        close(){this.readyState=3}
      }
      window.EventSource=Events;window.WebSocket=Socket;
    },lines);
    await page.route('**/api/**',route=>{
      const request=route.request(),url=new URL(request.url()),path=decodeURIComponent(url.pathname);
      if(path==='/api/bootstrap')return route.fulfill({json:{authenticated:true,needsSetup:false,username:'drawer-fixture',csrfToken:'fixture'}});
      if(path==='/api/projects')return route.fulfill({json:{projects,collectedAt:'2026-10-20T11:37:00Z'}});
      if(path.includes('/configuration/'))return route.fulfill({json:{available:false,state:'ready',directory:'/external/original-compose',files:[],variables:[]}});
      if(path==='/api/managed/settings')return route.fulfill({json:{projectsBase:'/srv/projects'}});
      if(path==='/api/self-update')return route.fulfill({json:{automatic:false,intervalMinutes:15,status:'up_to_date',lastChecked:'2026-10-20T11:37:00Z',currentBuildSHA:'123456789abcdef'}});
      if(path==='/api/jobs')return route.fulfill({json:[]});
      if(path.endsWith('/schedule'))return route.fulfill({json:{enabled:true,targetID:'compose:single',time:'03:00',timezone:'Europe/Lisbon',eligible:true,nextAt:1792530000,lastOutcome:'unchanged'}});
      if(path.endsWith('/logs/export')){
        const target=path.split('/')[3],name=target==='compose:single'?'single':target==='compose:multi'?'multi':'standalone';
        if(request.method()==='POST'){
          exports++;const body=request.postDataJSON();
          if(!body.confirm||body.content!==lines.join('\n')+'\n'||!ids.includes(body.containerID)||body.content.includes('<mark'))throw Error('Export did not preserve the reviewed plain-text buffer');
          if(failOnce){failOnce=false;return route.fulfill({status:409,json:{error:'Export directory is temporarily unavailable.'}})}
          saved.set(name,{filename:'2026-10-20-11-37.txt',exportedAt:'2026-10-20T11:37:00Z'});
        }
        return route.fulfill({status:request.method()==='POST'?201:200,json:{available:true,directory:'/srv/projects/'+name+'/logs',...saved.get(name)}});
      }
      if(path.startsWith('/api/containers/')){
        const id=path.split('/')[3],revealed=path.endsWith('/environment');
        return route.fulfill({json:{id,ports:Array.from({length:12},(_,i)=>({containerPort:String(8000+i)+'/tcp',hostIP:'127.0.0.1',hostPort:String(9000+i)})),mounts:[{type:'bind',source:'/srv/projects/single/data',destination:'/data',readOnly:false}],networks:[{name:'single_default',ipv4:'172.20.0.2'}],environment:Array.from({length:8},(_,i)=>({name:'VARIABLE_'+i,...(revealed?{value:'value-'+i}:{})}))}});
      }
      return route.fulfill({json:{}});
    });
    await page.emulateMedia({reducedMotion:'reduce'});await page.setViewportSize({width:1440,height:1000});await page.goto('http://127.0.0.1:5173');
    const openSingle=()=>page.getByRole('button',{name:/Compose running single/}).click();
    const drawer=page.getByRole('dialog',{name:'single',exact:true});
    // Keep desktop's preference across responsive menu use and breakpoint changes.
    await page.getByRole('button',{name:'Hide sidebar',exact:true}).click();
    await page.getByRole('button',{name:'Expand sidebar',exact:true}).waitFor();
    const touch=await page.context().newCDPSession(page);
    async function swipeMenu(type){
      const point=(x,y)=>[{x,y,id:1}];
      await touch.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:point(210,300)});
      await touch.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:point(type==='vertical'?208:190,type==='vertical'?370:300)});
      if(type==='close')await touch.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:point(35,300)});
      await touch.send('Input.dispatchTouchEvent',{type:type==='cancel'?'touchCancel':'touchEnd',touchPoints:[]});
    }
    for(const width of [1920,1440,1280,1279,1024,768,390,320]){
      await page.setViewportSize({width,height:1000});await openSingle();
      const box=await drawer.boundingBox();
      if(width>=1280&&box.width>width/2+1)throw Error('Desktop drawer exceeds half the screen');
      if(width<768&&Math.abs(box.width-width)>1)throw Error('Mobile drawer lost fullscreen');
      if(width>=768&&width<1280&&box.width<Math.min(896,width*.85))throw Error('Tablet drawer is too narrow');
      const close=drawer.getByRole('button',{name:'Close project details'}),icon=close.locator('i.ph-x');
      const buttonBox=await close.boundingBox(),iconBox=await icon.boundingBox();
      if(Math.abs(buttonBox.x+buttonBox.width/2-iconBox.x-iconBox.width/2)>1||Math.abs(buttonBox.y+buttonBox.height/2-iconBox.y-iconBox.height/2)>1)throw Error('Close icon is off-center');
      await close.click();
      const trigger=page.getByRole('button',{name:'Open menu',exact:true});
      if(width<1280){
        const menu=page.getByRole('dialog',{name:'Navigation menu',exact:true});
        const heading=await page.getByRole('heading',{name:'Projects',exact:true}).boundingBox(),triggerBox=await trigger.boundingBox();
        if(!triggerBox||triggerBox.x+triggerBox.width>heading.x)throw Error('Menu trigger is not beside Projects at '+width);
        await trigger.click();await menu.waitFor();
        if(!await page.locator('#content').evaluate(el=>el.parentElement.inert)||!await page.evaluate(()=>getComputedStyle(document.documentElement).overflow==='hidden'))throw Error('Open menu did not isolate background at '+width);
        const closeMenu=menu.getByRole('button',{name:'Close menu',exact:true});
        if(!await closeMenu.evaluate(el=>el===document.activeElement))throw Error('Menu did not receive focus');
        await page.keyboard.press('Shift+Tab');
        if(!await menu.getByRole('button',{name:'Sign out',exact:true}).evaluate(el=>el===document.activeElement))throw Error('Menu keyboard focus escaped');
        await page.keyboard.press('Tab');await page.keyboard.press('Escape');await menu.waitFor({state:'hidden'});
        await page.waitForFunction(()=>document.activeElement===document.querySelector('button[aria-label="Open menu"]'));
        await trigger.click();await page.mouse.click(width-12,220);await menu.waitFor({state:'hidden'});
        await trigger.click();
        if(width===1024){
          await swipeMenu('cancel');if(!await menu.isVisible())throw Error('Canceled swipe closed menu');
          await swipeMenu('vertical');if(!await menu.isVisible())throw Error('Vertical scroll closed menu');
          await page.screenshot({path:'output/playwright/sidebar-overlay-1024.png'});
        }
        if(width===320)await page.screenshot({path:'output/playwright/sidebar-overlay-320.png'});
        await swipeMenu('close');await menu.waitFor({state:'hidden'});
        await page.waitForFunction(()=>document.activeElement===document.querySelector('button[aria-label="Open menu"]'));
        await trigger.click();
      }else{
        if(await trigger.isVisible())throw Error('Overlay trigger appeared on wide desktop');
        await page.getByRole('button',{name:'Expand sidebar',exact:true}).waitFor();
      }
      await page.getByRole('button',{name:'Settings',exact:true}).click();
      const settings=page.getByRole('dialog',{name:'Settings',exact:true});
      if(Math.abs((await settings.boundingBox()).width-box.width)>1)throw Error('Settings drawer has a different width');
      await settings.getByText('Current build',{exact:true}).waitFor();
      if(await settings.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Settings overflow at '+width);
      if(width===1440||width===768||width===320)await page.screenshot({path:`output/playwright/settings-${width}.png`});
      await settings.getByRole('button',{name:'Close settings'}).click();
      await page.getByRole('button',{name:'New Project',exact:true}).click();
      const creation=page.getByRole('dialog',{name:'New Project',exact:true});
      if(Math.abs((await creation.boundingBox()).width-box.width)>1)throw Error('New Project drawer has a different width');
      await creation.getByRole('button',{name:'Close New Project'}).click();
    }
    await page.setViewportSize({width:1024,height:1000});await page.getByRole('button',{name:'Open menu',exact:true}).click();
    await page.setViewportSize({width:1440,height:1000});
    await page.waitForFunction(()=>!document.querySelector('[data-menu-open="true"]')&&!document.querySelector('#content').parentElement.inert);
    await page.getByRole('button',{name:'Expand sidebar',exact:true}).click();
    await touch.detach();
    await page.setViewportSize({width:1440,height:1000});await openSingle();
    await drawer.getByRole('region',{name:'Project overview'}).waitFor();
    const tabs=drawer.getByRole('tab');if(await tabs.count()!==3)throw Error('Drawer lost tabs');
    if(!await tabs.first().evaluate(el=>parseFloat(getComputedStyle(el).fontSize)>=16&&Number(getComputedStyle(el).fontWeight)>=600))throw Error('Tabs lack visual emphasis');
    const actions=await drawer.getByRole('button',{name:'Start single',exact:true}).boundingBox();
    const schedule=await drawer.getByRole('heading',{name:'Automatic image updates'}).boundingBox();
    if(actions.y>=schedule.y)throw Error('Immediate actions appear after settings');
    await page.screenshot({path:'output/playwright/drawer-project-1440.png'});
    await drawer.getByRole('button',{name:/api api-1 running/}).click();
    const ports=drawer.getByRole('region',{name:'Ports',exact:true});await ports.getByRole('listitem').last().waitFor();
    const positions=await ports.getByRole('listitem').evaluateAll(items=>items.map(el=>{const r=el.getBoundingClientRect();return{x:r.x,y:r.y}}));
    if(positions[0].y!==positions[1].y||positions[1].x<=positions[0].x||positions[2].y<positions[1].y)throw Error('Ports are not in horizontal reading order');
    if(await drawer.getByText('value-0',{exact:true}).count())throw Error('Environment values were disclosed implicitly');
    await drawer.getByRole('button',{name:'Reveal values',exact:true}).click();await drawer.getByText('value-0',{exact:true}).waitFor();
    await page.screenshot({path:'output/playwright/drawer-container-1440.png'});
    await drawer.getByRole('button',{name:'Back to project',exact:true}).click();await drawer.getByRole('region',{name:'Project overview'}).waitFor();
    await drawer.getByRole('tab',{name:'Logs',exact:true}).click();
    const output=drawer.locator('pre[aria-label="api log output"]');await output.getByText(/ready line 89/).waitFor();
    await drawer.getByRole('searchbox',{name:'Search logs',exact:true}).fill('ERROR');
    if(await output.locator('mark').count()!==2)throw Error('Log search did not highlight both matches');
    if(!await output.textContent().then(text=>text.includes('ready line 0')&&text.includes('ready line 89')))throw Error('Search filtered the log buffer');
    await drawer.getByRole('button',{name:'Next log match'}).click();
    if(await output.locator('mark[data-active="true"]').getAttribute('data-log-match')!=='1')throw Error('Log match navigation failed');
    await drawer.getByRole('searchbox',{name:'Search logs',exact:true}).fill('[a+b]');
    if(await output.locator('mark').count()!==1)throw Error('Log search interpreted regular-expression syntax');
    await drawer.getByRole('searchbox',{name:'Search logs',exact:true}).fill('no-such-line');await drawer.getByText('No matches',{exact:true}).waitFor();
    await drawer.getByRole('button',{name:'Clear log search'}).click();
    for(const width of [1440,1024,768,390,320]){
      await page.setViewportSize({width,height:1000});
      if(await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Drawer overflows at '+width);
      const panel=await drawer.getByRole('tabpanel').boundingBox(),pre=await output.boundingBox();
      if(pre.height<panel.height*.45||pre.y+pre.height>1000)throw Error('Log output does not use the available drawer height at '+width);
      if(width===1440||width===320)await page.screenshot({path:`output/playwright/drawer-logs-${width}.png`});
    }
    await page.setViewportSize({width:1440,height:1000});
    const exportButton=drawer.getByRole('button',{name:'Export log',exact:true});await exportButton.click();
    const confirm=page.getByRole('dialog',{name:'Save current log?',exact:true});
    await confirm.getByText('/srv/projects/single/logs/YYYY-MM-DD-HH-mm.txt',{exact:true}).waitFor();
    await confirm.getByRole('button',{name:'Cancel',exact:true}).click();if(exports)throw Error('Canceled export saved a file');
    await exportButton.click();await confirm.getByRole('button',{name:'Save log',exact:true}).click();await confirm.getByRole('alert').waitFor();
    await confirm.getByRole('button',{name:'Save log',exact:true}).click();await confirm.waitFor({state:'hidden'});
    if(exports!==2||!await exportButton.evaluate(el=>el===document.activeElement))throw Error('Export retry/focus return failed');
    if(await drawer.getByText('Last export: Never',{exact:true}).count())throw Error('Last export did not update');
    await drawer.getByRole('tab',{name:'Terminal',exact:true}).click();await drawer.getByText('Connected',{exact:true}).waitFor();
    const surface=drawer.locator('[aria-label="api interactive terminal"]');
    if((await surface.boundingBox()).height<(await drawer.getByRole('tabpanel').boundingBox()).height*.65)throw Error('Terminal does not fill drawer height');
    await page.screenshot({path:'output/playwright/drawer-terminal-1440.png'});
    await drawer.getByRole('button',{name:'Close project details'}).click();await openSingle();await drawer.getByRole('tab',{name:'Logs',exact:true}).click();
    await drawer.getByText(/^Last export:/).waitFor();await page.waitForFunction(()=>Array.from(document.querySelectorAll('footer span')).some(el=>el.textContent.startsWith('Last export:')&&!/Never|Unavailable/.test(el.textContent)));
    await drawer.getByRole('button',{name:'Close project details'}).click();
    await page.getByRole('button',{name:/Compose running multi/}).click();const multi=page.getByRole('dialog',{name:'multi',exact:true});
    await multi.getByRole('region',{name:'Project overview'}).waitFor();if(await multi.getByRole('region',{name:'Containers',exact:true}).getByRole('button').count()!==2)throw Error('Multi-container project entry is inconsistent');
    await multi.getByRole('button',{name:'Close project details'}).click();
    await page.getByRole('button',{name:/Container running standalone/}).click();
    const standalone=page.getByRole('dialog',{name:'standalone',exact:true});await standalone.getByRole('tab',{name:'Logs',exact:true}).click();
    await standalone.getByRole('button',{name:'Export log',exact:true}).click();
    await confirm.getByText('/srv/projects/standalone/logs/YYYY-MM-DD-HH-mm.txt',{exact:true}).waitFor();
    await confirm.getByRole('button',{name:'Save log',exact:true}).click();await confirm.waitFor({state:'hidden'});
    if(!saved.has('standalone'))throw Error('Standalone export did not use its base/name/logs destination');
    if(errors.length)throw Error(errors.join('\n'));
    console.log('PASS: responsive overlay navigation with real touch swipe, keyboard focus and desktop preference preservation; uniform drawer sizes, centered Phosphor close, emphasized tabs, consistent Compose entry, row-major ports, private env values, full-height logs/terminal, literal highlighted search and confirmed plain-text host export with retry/persistence');
  } finally {await page.close()}
}
