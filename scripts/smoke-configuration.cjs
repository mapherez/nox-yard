/* Inline environment editing, source accordion, migration gating and Yard controls. */
async (page) => {
  await page.unrouteAll({behavior:'wait'});
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  const id='a'.repeat(64),yardID='b'.repeat(64);
  const metrics={cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null};
  const container=(id,service)=>({...metrics,id,name:service+'-1',service,image:'fixture/app',state:'running',health:'healthy',terminalAvailable:true});
  const completed=(id,service)=>({...container(id,service),state:'exited',completed:true,health:'none'});
  const projects=[{...metrics,id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'healthy',containers:[container(id,'web'),completed('c'.repeat(64),'state-init'),completed('d'.repeat(64),'volume-init')]},{...metrics,id:'compose:nox-yard',name:'nox-yard',kind:'external-compose',state:'running',health:'healthy',containers:[container(yardID,'nox-yard')]}];
  let state='migrating',writes=0,failOnce=true,currentValue='running-value',pending=false,checks=0;
  let compose='services:\n  web:\n    image: fixture/app\n    env_file: [app.env]\n';
  const view=(reveal)=>({available:state==='ready',state,directory:'/srv/sample',fingerprint:String(writes).padStart(64,'0'),files:state==='ready'?[{path:'compose.yaml',compose:true,content:reveal?compose:''},{path:'app.env',compose:false,content:reveal?'TOKEN='+currentValue+'\nOTHER=keep\n':''}]:[],variables:state==='ready'?[{name:'TOKEN',service:'web',value:reveal?currentValue:'',pending}]:[]});
  await page.route('**/api/**',route=>{
    const request=route.request(),path=new URL(request.url()).pathname;
    if(path==='/api/bootstrap')return route.fulfill({json:{authenticated:true,needsSetup:false,username:'owner',csrfToken:'fixture'}});
    if(path==='/api/projects/events')return route.fulfill({contentType:'text/event-stream',body:'data: {"inventory":false,"metrics":false}\n\n'});
    if(path==='/api/projects')return route.fulfill({json:{projects,collectedAt:'2026-10-15T20:00:00Z'}});
    if(path==='/api/jobs')return route.fulfill({json:[]});
    if(path==='/api/configuration-migration')return route.fulfill({json:[{name:'sample',state}]});
    if(path.endsWith('/configuration/read'))return route.fulfill({json:view(request.postDataJSON()?.reveal)});
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
  await page.emulateMedia({reducedMotion:'reduce'});await page.setViewportSize({width:1440,height:1000});await page.goto('http://127.0.0.1:5173');
  await page.getByRole('button',{name:/Managed Compose running sample/}).click();
  const drawer=page.getByRole('dialog',{name:'sample',exact:true});
  await drawer.getByRole('button',{name:/state-init state-init-1 completed/}).waitFor();
  await drawer.getByRole('button',{name:/volume-init volume-init-1 completed/}).waitFor();
  await drawer.getByText('Migrating configuration…',{exact:true}).waitFor();
  await drawer.getByLabel('More actions',{exact:true}).click();
  if(!await drawer.getByRole('button',{name:'Recreate',exact:true}).isDisabled())throw Error('migration did not gate Recreate');
  await drawer.getByLabel('More actions',{exact:true}).click();state='ready';
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
  await drawer.getByRole('button',{name:'Save TOKEN',exact:true}).click();
  await drawer.getByRole('alert').filter({hasText:'Files changed.'}).waitFor();
  if(await field.inputValue()!=='changed-value')throw Error('failed save lost draft');
  await drawer.getByRole('button',{name:'Save TOKEN',exact:true}).click();
  await drawer.getByText('Saved · Recreate required',{exact:true}).waitFor();
  if(writes!==1)throw Error('inline save did not persist exactly once');
  await accordion.locator('summary').click();
  const yaml=drawer.getByLabel('compose.yaml',{exact:true});
  try { await yaml.waitFor({timeout:5000}); } catch { throw Error('Compose editor did not open: '+await accordion.evaluate(el=>el.outerHTML)); }
  if(await yaml.inputValue()!==compose)throw Error('current Compose text not loaded');
  await yaml.fill(compose+'# edited in place\n');
  await yaml.locator('xpath=ancestor::form').getByRole('button',{name:'Save',exact:true}).click();
  await drawer.getByText('Saved. Use Recreate to apply the changes.',{exact:true}).waitFor();
  if(writes!==2)throw Error('Compose save missing');
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
  await settings.getByRole('heading',{name:'NoX Yard updates',exact:true}).waitFor();
  await settings.getByText('Current build',{exact:true}).waitFor();
  if(await settings.getByRole('radio').count()||await settings.getByRole('checkbox').count())throw Error('old interval/automatic controls remain in Settings');
  await settings.getByRole('button',{name:'Check now',exact:true}).click();if(checks!==1)throw Error('Check now did not use manual path');
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: migration gating, inline env edit/save/cancel semantics, stale draft, collapsed Compose editor, uniform Yard actions, daily schedule, simplified Settings and responsive layout');
}
