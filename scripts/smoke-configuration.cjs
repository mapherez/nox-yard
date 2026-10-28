/* On-demand file editing without migration polling; source and Yard controls. */
async (page) => {
  await page.unrouteAll({behavior:'wait'});
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.addInitScript(() => {
    window.configurationStreams=[];
    window.EventSource=class extends EventTarget {
      static OPEN=1;
      constructor(url){super();this.url=url;this.readyState=1;window.configurationStreams.push(this)}
      close(){this.readyState=2}
    };
  });
  const id='a'.repeat(64),yardID='b'.repeat(64);
  const metrics={cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null};
  const container=(id,service)=>({...metrics,id,name:service+'-1',service,image:'fixture/app',state:'running',health:'healthy',terminalAvailable:true});
  const completed=(id,service)=>({...container(id,service),state:'exited',completed:true,health:'none'});
  const projects=[{...metrics,id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'healthy',containers:[container(id,'web'),completed('c'.repeat(64),'state-init'),completed('d'.repeat(64),'volume-init')]},{...metrics,id:'compose:nox-yard',name:'nox-yard',kind:'external-compose',state:'running',health:'healthy',containers:[container(yardID,'nox-yard')]}];
  let writes=0,failOnce=true,currentValue='running-value',pending=false,checks=0,servicePreviews=0,serviceSubmits=0,migrationRequests=0,configurationReads=0;
  let inventory=projects,inventoryReads=0;
  let compose='services:\n  web:\n    image: fixture/app\n    env_file: [app.env]\n';
  const view=(reveal)=>({available:true,directory:'/srv/sample',fingerprint:String(writes).padStart(64,'0'),files:[{path:'compose.yaml',compose:true,content:reveal?compose:''},{path:'app.env',compose:false,content:reveal?'TOKEN='+currentValue+'\nOTHER=keep\n':''}],variables:[{name:'TOKEN',service:'web',value:reveal?currentValue:'',pending}]});
  await page.route('**/api/**',route=>{
    const request=route.request(),path=new URL(request.url()).pathname;
    if(path==='/api/bootstrap')return route.fulfill({json:{authenticated:true,needsSetup:false,username:'owner',csrfToken:'fixture'}});
    if(path==='/api/projects/events')return route.fulfill({contentType:'text/event-stream',body:'data: {"inventory":false,"metrics":false}\n\n'});
    if(path==='/api/projects'){inventoryReads++;return route.fulfill({json:{projects:inventory,collectedAt:'2026-10-15T20:00:00Z'}})}
    if(path==='/api/metrics')return route.fulfill({json:{[id]:{...metrics,cpuPercent:42,collectedAt:'2026-10-15T20:00:05Z'}}});
    if(path==='/api/jobs')return route.fulfill({json:[]});
    if(path==='/api/recreate/preview') {
      const body=request.postDataJSON();if(body.id!=='compose:sample'||body.service!=='web'||body.operation!=='recreate')throw Error('managed container recreation widened to project');
      servicePreviews++;
      return route.fulfill({json:{fingerprint:'f'.repeat(64),order:['web'],items:[{id,name:'web-1',service:'web',image:'fixture/app',imageID:'sha256:fixture',running:true,preserved:['volumes','environment files'],environment:['TOKEN']}]}});
    }
    if(path==='/api/recreate/submit') {
      const body=request.postDataJSON();if(body.id!=='compose:sample'||body.service!=='web'||body.operation!=='recreate'||body.fingerprint!=='f'.repeat(64)||!body.confirm)throw Error('managed service submission lost reviewed scope');
      serviceSubmits++;return route.fulfill({status:202,json:{id:'service-job',projectName:'sample',targetID:'compose:sample',operation:'recreate',status:'succeeded'}});
    }
    if(path.includes('configuration-migration')||path.endsWith('/retry-migration')){migrationRequests++;return route.fulfill({status:404,json:{error:'Removed'}})}
    if(path.endsWith('/configuration/read')){configurationReads++;return route.fulfill({json:view(request.postDataJSON()?.reveal)})}
    if(path.endsWith('/configuration/save')){
      const body=request.postDataJSON();if(body.fingerprint!==String(writes).padStart(64,'0'))throw Error('wrong configuration fingerprint');
      if(failOnce){failOnce=false;return route.fulfill({status:409,json:{error:'Files changed. Reload before saving.'}})}
      writes++;if(body.variable){if(body.variable!=='TOKEN'||body.service!=='web')throw Error('wrong variable target');currentValue=body.content;pending=true}else{if(body.path!=='compose.yaml')throw Error('wrong Compose path');compose=body.content}
      return route.fulfill({json:view(true)});
    }
    if(path.includes('/api/containers/'))return route.fulfill({json:{id:path.includes(yardID)?yardID:id,ports:[],mounts:[],networks:[],environment:[{name:'TOKEN',...(path.endsWith('/environment')?{value:'running-value'}:{})}]}});
    if(path.endsWith('/schedule'))return route.fulfill({json:{enabled:false,time:'03:00',timezone:'Europe/Lisbon',eligible:true}});
    if(path==='/api/self-update')return route.fulfill({json:{automatic:false,intervalMinutes:15,status:'up_to_date',currentBuildSHA:'123456789abcdef',lastChecked:'2026-10-15T19:00:00Z'}});
    if(path==='/api/self-update/check'){checks++;return route.fulfill({status:202,json:{status:'checking'}})}
    if(path==='/api/managed/settings')return route.fulfill({json:{projectsBase:'/srv/projects'}});
    return route.fulfill({json:{}});
  });
  await page.clock.install();
  await page.emulateMedia({reducedMotion:'reduce'});await page.setViewportSize({width:1440,height:1000});await page.goto('http://127.0.0.1:5173');
  await page.getByRole('button',{name:/Managed Compose running sample/}).click();
  const drawer=page.getByRole('dialog',{name:'sample',exact:true});
  await drawer.getByRole('button',{name:/state-init state-init-1 completed/}).waitFor();
  await drawer.getByRole('button',{name:/volume-init volume-init-1 completed/}).waitFor();
  await drawer.getByRole('button',{name:/web web-1 running/}).click();
  await drawer.getByRole('button',{name:'Edit TOKEN',exact:true}).waitFor();
  const accordion=drawer.locator('details').filter({has:page.locator('summary').filter({hasText:'Compose and environment files'})});
  if(await accordion.getAttribute('open')!==null)throw Error('Compose accordion must start closed');
  await drawer.getByRole('button',{name:'Edit TOKEN',exact:true}).click();
  // Hidden values use password controls, which do not have textbox role.
  const field=drawer.getByLabel('Value for TOKEN',{exact:true});
  await field.waitFor();if(await field.inputValue()!=='running-value')throw Error('existing env value was not loaded');
  await field.fill('cancelled-value');
  await drawer.getByRole('button',{name:'Cancel editing TOKEN',exact:true}).click();
  if(writes!==0)throw Error('cancel saved implicitly');
  await drawer.getByRole('button',{name:'Edit TOKEN',exact:true}).click();
  if(await field.inputValue()!=='running-value')throw Error('cancel changed saved value');
  await field.fill('changed-value');if(writes!==0)throw Error('typing saved implicitly');
  await drawer.evaluate(el=>el.close());
  await page.waitForFunction(()=>document.querySelector('#project-drawer')?.open);
  if(await field.inputValue()!=='changed-value')throw Error('Unexpected close discarded the env-only draft');
  await accordion.locator('summary').click();
  const draftYaml=drawer.getByLabel('compose.yaml',{exact:true});
  await draftYaml.fill(compose+'# unsaved draft\n');
  const envFile=drawer.getByLabel('app.env',{exact:true});
  const envText=await envFile.inputValue();
  await envFile.press('Control+a');
  await envFile.press('Control+c');
  await envFile.press('Control+v');
  if(!await drawer.isVisible()||await envFile.inputValue()!==envText)throw Error('Copy/paste in the env file closed or changed the editor');
  await draftYaml.focus();
  await draftYaml.press('Control+a');
  await draftYaml.press('Control+v');
  if(!await drawer.isVisible()||await draftYaml.inputValue()!==envText)throw Error('Pasting multiline env content into Compose lost the editor');
  await draftYaml.press('Control+a');
  await draftYaml.press('Control+c');
  await draftYaml.press('Control+x');
  await draftYaml.press('Control+v');
  if(!await drawer.isVisible()||await draftYaml.inputValue()!==envText)throw Error('Cut/paste in Compose lost the editor');
  await envFile.focus();
  await envFile.evaluate(el=>el.setSelectionRange(0,5));
  await envFile.press('Control+c');
  await field.press('Control+a');
  await field.press('Control+v');
  if(!await drawer.isVisible()||await field.inputValue()!==envText.slice(0,5))throw Error('Pasting an inline env value lost the editor');
  await field.fill('changed-value');
  await draftYaml.fill(compose+'# unsaved draft\n');
  const selectionBox=await draftYaml.boundingBox();
  const drawerBox=await drawer.boundingBox();
  await page.mouse.move(selectionBox.x+30,selectionBox.y+30);
  await page.mouse.down();
  await page.mouse.move(drawerBox.x-30,selectionBox.y+30,{steps:8});
  await page.mouse.up();
  if(!await drawer.isVisible()||await draftYaml.inputValue()!==compose+'# unsaved draft\n')throw Error('Dragging a text selection beyond the drawer discarded Compose');
  await draftYaml.focus();
  const editorState=await draftYaml.evaluate(el=>{
    el.setSelectionRange(4,12);el.scrollTop=10;
    const panel=el.closest('[role="tabpanel"]');panel.scrollTop=200;
    return {selectionStart:el.selectionStart,selectionEnd:el.selectionEnd,scrollTop:el.scrollTop,panelScrollTop:panel.scrollTop};
  });
  const editorNode=await draftYaml.elementHandle();
  const readsBeforeRefresh=configurationReads;
  async function refreshInventory(next){
    inventory=next;
    const response=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/projects');
    await page.evaluate(()=>window.configurationStreams.find(stream=>stream.url==='/api/projects/events').dispatchEvent(new MessageEvent('change',{data:JSON.stringify({inventory:true})})));
    await response;
    await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  }
  async function checkDrafts(){
    if(!await drawer.isVisible()||await field.inputValue()!=='changed-value'||await draftYaml.inputValue()!==compose+'# unsaved draft\n')throw Error('Inventory refresh lost an open editor or its drafts');
    if(!await editorNode.evaluate((el,expected)=>el.isConnected&&el===document.activeElement&&el.selectionStart===expected.selectionStart&&el.selectionEnd===expected.selectionEnd&&el.scrollTop===expected.scrollTop&&el.closest('[role="tabpanel"]').scrollTop===expected.panelScrollTop,editorState))throw Error('Refresh moved focus, cursor, scroll or remounted the Compose editor');
    if(configurationReads!==readsBeforeRefresh||writes!==0)throw Error('Background refresh reloaded or saved a dirty configuration');
  }
  // Exercise the browser's dismissal paths, not just data refreshes.
  await page.mouse.click(20,300);
  if(!await drawer.isVisible())throw Error('Backdrop dismissed the drawer with unsaved configuration');
  await draftYaml.focus();
  await page.keyboard.press('Escape');
  if(!await drawer.isVisible())throw Error('Escape discarded the active configuration editor');
  await draftYaml.focus();
  await draftYaml.evaluate(el=>el.setSelectionRange(4,12));
  await checkDrafts();
  await drawer.evaluate(el=>el.close());
  await page.waitForFunction(()=>document.querySelector('#project-drawer')?.open);
  await checkDrafts();
  // Simulate the cancel event for browsers that ignore closedby="none".
  if(await drawer.evaluate(el=>el.dispatchEvent(new Event('cancel',{cancelable:true}))))throw Error('Cancel event was not prevented while editing');
  await checkDrafts();
  const touch=await page.context().newCDPSession(page);
  const bounds=await drawer.boundingBox();
  await touch.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:bounds.x+80,y:bounds.y+40,id:1}]});
  await touch.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:bounds.x+280,y:bounds.y+40,id:1}]});
  await touch.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
  await checkDrafts();
  await refreshInventory(projects.map(project=>({...project,cpuPercent:21,containers:project.containers.map(container=>({...container,cpuPercent:21}))})));
  await drawer.getByText('21.0%',{exact:true}).waitFor();
  await checkDrafts();
  await page.evaluate(()=>window.configurationStreams.find(stream=>stream.url==='/api/projects/events').dispatchEvent(new MessageEvent('change',{data:JSON.stringify({metrics:true})})));
  await drawer.getByText('42.0%',{exact:true}).waitFor();
  await checkDrafts();
  await refreshInventory([]);
  await drawer.getByText('This project is missing from the latest inventory. Your unsaved edits are kept.',{exact:true}).waitFor();
  await checkDrafts();
  await refreshInventory(projects.map(project=>({...project,containers:[]})));
  await drawer.getByText('This container is missing from the latest inventory. Your unsaved edits are kept.',{exact:true}).waitFor();
  await checkDrafts();
  await refreshInventory(projects.map(project=>({...project,operation:'restarting',containers:project.containers.map(container=>({...container,operation:'restarting'}))})));
  await drawer.getByText('restarting',{exact:true}).first().waitFor();
  await checkDrafts();
  await refreshInventory(projects);
  await checkDrafts();
  // Fallback reconciliation must also leave the active editors untouched.
  const inventoryBeforePolling=inventoryReads;
  for(let i=0;i<8;i++){await page.clock.fastForward(15_000);await page.waitForTimeout(50)}
  if(inventoryReads<=inventoryBeforePolling)throw Error('Reconciliation did not run while editing');
  await checkDrafts();
  await page.clock.resume();
  // Keep the existing stale-fingerprint/retry checks with both drafts active.
  await drawer.getByRole('button',{name:'Save TOKEN',exact:true}).click();
  await drawer.getByRole('alert').filter({hasText:'Files changed.'}).first().waitFor();
  if(await field.inputValue()!=='changed-value')throw Error('failed save lost draft');
  await drawer.getByRole('button',{name:'Save TOKEN',exact:true}).click();
  await drawer.getByText('Saved · Recreate required',{exact:true}).waitFor();
  if(writes!==1)throw Error('inline save did not persist exactly once');
  const yaml=drawer.getByLabel('compose.yaml',{exact:true});
  try { await yaml.waitFor({timeout:5000}); } catch { throw Error('Compose editor did not open: '+await accordion.evaluate(el=>el.outerHTML)); }
  if(await yaml.inputValue()!==compose+'# unsaved draft\n')throw Error('Saving an env variable overwrote the Compose draft');
  await yaml.fill(compose+'# edited in place\n');
  const reconciled=page.waitForResponse(response=>new URL(response.url()).pathname.endsWith('/configuration/read'));
  await yaml.locator('xpath=ancestor::form').getByRole('button',{name:'Save',exact:true}).click();
  await drawer.getByText('Saved. Use Recreate to apply the changes.',{exact:true}).waitFor();
  await reconciled;
  if(configurationReads<=readsBeforeRefresh||await yaml.inputValue()!==compose)throw Error('Deferred configuration refresh did not resume after saving all drafts');
  if(writes!==2)throw Error('Compose save missing');
  await drawer.getByLabel('More actions',{exact:true}).click();
  await drawer.getByRole('button',{name:'Recreate',exact:true}).click();
  const serviceDialog=page.getByRole('dialog',{name:'Recreate service?',exact:true});
  await serviceDialog.getByRole('button',{name:'Confirm recreate',exact:true}).waitFor();
  await serviceDialog.getByText('sample / web',{exact:true}).waitFor();
  if(await serviceDialog.getByRole('listitem').count()!==1)throw Error('service preview includes initializers');
  for(const width of [1440,768,390,320]){await page.setViewportSize({width,height:1000});if(await serviceDialog.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('service confirmation overflows '+width)}
  await page.keyboard.press('Escape');await serviceDialog.waitFor({state:'hidden'});
  if(serviceSubmits!==0)throw Error('cancel submitted service recreation');
  await drawer.getByLabel('More actions',{exact:true}).click();await drawer.getByRole('button',{name:'Recreate',exact:true}).click();
  await serviceDialog.getByRole('button',{name:'Confirm recreate',exact:true}).click();await serviceDialog.waitFor({state:'hidden'});
  if(servicePreviews!==2||serviceSubmits!==1)throw Error('service recreate not submitted once with scope');
  for(const width of [1440,768,390,320]){await page.setViewportSize({width,height:1000});if(await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('configuration overflows '+width);if(width===1440||width===320)await page.screenshot({path:'output/playwright/configuration-'+width+'.png'})}
  await page.keyboard.press('Escape');await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:/Compose running nox-yard/}).click();
  const yard=page.getByRole('dialog',{name:'nox-yard',exact:true});
  await yard.getByLabel('Daily check time (Europe/Lisbon)').waitFor();
  await yard.getByLabel('More actions',{exact:true}).click();
  if(await yard.getByRole('button',{name:'Pull',exact:true}).isDisabled()||await yard.getByRole('button',{name:'Recreate',exact:true}).isDisabled())throw Error('Yard lacks uniform actions');
  if(await yard.getByRole('button',{name:/^Update/}).count())throw Error('redundant Update action remains');
  await page.keyboard.press('Escape');await yard.getByRole('button',{name:'Close project details',exact:true}).click();await page.getByRole('button',{name:'Settings',exact:true}).click();
  const settings=page.getByRole('dialog',{name:'Settings',exact:true});
  await settings.getByRole('heading',{name:'Yard updates',exact:true}).waitFor();
  await settings.getByText('Current build',{exact:true}).waitFor();
  if(await settings.getByRole('radio').count()||await settings.getByRole('checkbox').count())throw Error('old interval/automatic controls remain in Settings');
  await settings.getByRole('button',{name:'Check now',exact:true}).click();if(checks!==1)throw Error('Check now did not use manual path');
  const reads=configurationReads;
  await page.clock.fastForward(30_000);
  if(migrationRequests||configurationReads!==reads)throw Error('Retired migration or configuration polling is still running');
  await settings.getByRole('button',{name:'Close settings',exact:true}).click();
  await page.getByRole('button',{name:/Managed Compose running sample/}).click();
  await drawer.getByRole('button',{name:/web web-1 running/}).click();
  await drawer.getByRole('button',{name:'Edit TOKEN',exact:true}).click();
  await field.fill('explicitly-discarded');
  await drawer.getByRole('button',{name:'Close project details',exact:true}).click();
  await drawer.waitFor({state:'hidden'});
  await page.getByRole('button',{name:/Managed Compose running sample/}).click();
  if(await field.count())throw Error('Explicitly closed draft leaked into the next editor');
  await page.mouse.click(20,300);
  await drawer.waitFor({state:'hidden'});
  if(errors.length)throw Error(errors.join('\n'));
  return 'PASS: unexpected close, backdrop, Escape, cancel and touch cannot discard active env/Compose edits; live refresh preserves focus/cursor/scroll; explicit close and idle backdrop work; conflict retry and saves preserved';
}
