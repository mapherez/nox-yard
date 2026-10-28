/* Real document reloads across deployments; API/HTML fixtures, no Docker changes. */
async (page) => {
  const url='http://127.0.0.1:5173';
  const shell=await (await page.request.get(url)).text();
  let version='v1',assets=1,available=true,oldDocument=null,documents=0,checks=0;
  const id='a'.repeat(64);
  const metrics={cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null};
  const project={...metrics,id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'healthy',containers:[{...metrics,id,name:'web-1',service:'web',image:'fixture/app',state:'running',health:'healthy',terminalAvailable:true}]};
  await page.addInitScript(()=>{
    window.interfaceStreams=[];
    window.EventSource=class extends EventTarget {
      static OPEN=1;
      constructor(url){super();this.url=url;this.readyState=1;window.interfaceStreams.push(this)}
      close(){this.readyState=2}
    };
    sessionStorage.setItem('interface-loads',String(Number(sessionStorage.getItem('interface-loads')||0)+1));
  });
  await page.route('**/',route=>{
    const navigation=route.request().isNavigationRequest();
    if(navigation)documents++;else checks++;
    if(!available)return route.fulfill({status:503,contentType:'text/plain',body:'Restarting'});
    const served=navigation&&oldDocument?oldDocument:version;
    if(navigation)oldDocument=null;
    const body=shell.replace('</head>',`<meta name="yard-version" content="${served}"></head>`).replace(/(src="\/assets\/[^"?]+\.js)"/g,`$1?revision=${assets}"`);
    return route.fulfill({contentType:'text/html',headers:{'Cache-Control':'no-cache'},body});
  });
  await page.route('**/api/**',route=>{
    const request=route.request(),path=new URL(request.url()).pathname;
    if(path==='/api/bootstrap')return route.fulfill({json:{authenticated:true,needsSetup:false,username:'owner',csrfToken:'fixture'}});
    if(path==='/api/projects')return route.fulfill({json:{projects:[project],collectedAt:'2026-10-28T12:00:00Z'}});
    if(path==='/api/jobs')return route.fulfill({json:[]});
    if(path.endsWith('/configuration/read')){
      const reveal=request.postDataJSON()?.reveal;
      return route.fulfill({json:{available:true,directory:'/srv/sample',fingerprint:'0'.repeat(64),files:[{path:'compose.yaml',compose:true,content:reveal?'services:\n  web:\n    image: fixture/app\n':''}],variables:[{name:'TOKEN',service:'web',value:reveal?'saved-token':'',pending:false}]}});
    }
    if(path.startsWith('/api/containers/'))return route.fulfill({json:{id,ports:[],mounts:[],networks:[],environment:[{name:'TOKEN'}]}});
    if(path.endsWith('/schedule'))return route.fulfill({json:{enabled:false,time:'03:00',timezone:'Europe/Lisbon',eligible:true}});
    return route.fulfill({json:{}});
  });
  await page.clock.install();
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.goto(url);
  await page.getByRole('heading',{name:'Projects',exact:true}).waitFor();
  await page.waitForTimeout(100);
  async function check(){
    const response=page.waitForResponse(response=>new URL(response.url()).pathname==='/'&&!response.request().isNavigationRequest());
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
    await response;
    await page.waitForTimeout(100);
  }
  async function reloaded(trigger){
    const loaded=page.waitForEvent('load');
    await trigger();await loaded;
    await page.getByRole('heading',{name:'Projects',exact:true}).waitFor();
    await page.waitForTimeout(100);
    if(await page.locator('meta[name="yard-version"]').getAttribute('content')!==version)throw Error('Reload did not load the deployed interface');
  }
  await check();
  if(documents!==1)throw Error('Unchanged interface caused a reload loop');
  available=false;
  await check();
  if(documents!==1)throw Error('Server outage navigated away from the working interface');
  available=true;version='v2';
  await reloaded(()=>page.evaluate(()=>window.interfaceStreams.find(stream=>stream.url==='/api/projects/events').dispatchEvent(new Event('open'))));
  await check();if(documents!==2)throw Error('Reconnect must reload once for a new release');

  const openProject=()=>page.getByRole('button',{name:/Managed Compose running sample/}).click();
  const drawer=page.getByRole('dialog',{name:'sample',exact:true});
  await openProject();await drawer.getByRole('button',{name:/web web-1 running/}).click();
  await drawer.getByRole('button',{name:'Edit TOKEN',exact:true}).click();
  const env=drawer.getByLabel('Value for TOKEN',{exact:true});await env.fill('unsaved-token');
  await drawer.locator('summary').filter({hasText:'Compose and environment files'}).click();
  const yaml=drawer.getByLabel('compose.yaml',{exact:true});await yaml.fill('services: {} # unsaved');
  version='v3';await check();
  for(let i=0;i<3;i++){await page.clock.fastForward(15_000);await page.waitForTimeout(50)}
  if(documents!==2||await env.inputValue()!=='unsaved-token'||await yaml.inputValue()!=='services: {} # unsaved')throw Error('Deployment/polling discarded an active editor');
  await drawer.getByRole('button',{name:'Close project details',exact:true}).click();
  const warning=page.getByRole('dialog',{name:'Unsaved changes',exact:true});
  await warning.getByRole('button',{name:'No',exact:true}).click();
  if(documents!==2||await yaml.inputValue()!=='services: {} # unsaved')throw Error('Cancelling discard applied a pending interface reload');
  await drawer.getByRole('button',{name:'Close project details',exact:true}).click();
  await reloaded(()=>warning.getByRole('button',{name:'Yes',exact:true}).click());
  if(documents!==3)throw Error('Pending deployment did not reload automatically after leaving the editor');

  await openProject();
  version='v4';await check();
  version='v3';
  await drawer.getByRole('button',{name:'Close project details',exact:true}).click();
  await page.waitForTimeout(150);
  if(documents!==3)throw Error('Rolled-back pending release caused an unnecessary reload');
  assets=2;
  await reloaded(()=>page.evaluate(()=>window.dispatchEvent(new Event('online'))));
  if(documents!==4||!await page.locator('script[type="module"][src]').getAttribute('src').then(src=>src.includes('revision=2')))throw Error('Changed frontend assets were missed when the version stayed the same');
  await check();if(documents!==4)throw Error('Fresh interface kept reloading');

  // Login/setup forms are also protected while a user is typing.
  await page.evaluate(()=>{const form=document.createElement('form');const input=document.createElement('input');input.id='in-progress-form';form.append(input);document.body.append(form);input.focus();input.value='unsaved';});
  version='v4';await check();
  if(documents!==4)throw Error('Deployment interrupted an active form');
  await reloaded(()=>page.evaluate(()=>document.querySelector('#in-progress-form').parentElement.remove()));
  if(documents!==5)throw Error('Update did not resume after the form was completed');

  // Compare with the loaded document, not the first response from the server.
  version='v5';oldDocument='v4';
  await page.goto(url,{waitUntil:'domcontentloaded'});
  await page.waitForFunction(()=>document.querySelector('meta[name="yard-version"]')?.content==='v5');
  await page.getByRole('heading',{name:'Projects',exact:true}).waitFor();
  if(documents!==7)throw Error('A deployment before the first version check was missed');
  if(checks<5||Number(await page.evaluate(()=>sessionStorage.getItem('interface-loads')))!==documents)throw Error('Tests did not execute real document reloads');
  return 'PASS: real automatic reloads on release/asset changes and reconnect; no loops or outage reloads; env/Compose and focused forms preserved until completion; rollback rechecked; already-stale startup detected';
}
