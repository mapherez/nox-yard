/* Run with playwright-cli run-code --filename scripts/smoke-recreate.cjs. Local Vite required. */
async (page) => {
  const container = (id,service) => ({id,name:`sample-${service}-1`,service,image:'example/app:latest',state:'running',health:'healthy',terminalAvailable:true,cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null});
  const project={id:'compose:sample',name:'sample',kind:'external-compose',state:'running',health:'healthy',cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null,containers:[container('a'.repeat(64),'db'),container('b'.repeat(64),'web')]};
  let submits=0,previews=0,stale=true,unsupported=false,removals=0,lastRemoval=null,history=[];
  await page.unrouteAll({behavior:'wait'});
  await page.route('**/api/**', async route => {
    const request=route.request(),url=new URL(request.url()),path=url.pathname;
    if(path==='/api/bootstrap') return route.fulfill({json:{authenticated:true,needsSetup:false,username:'admin',csrfToken:'fixture'}});
    if(path==='/api/projects/events') return route.fulfill({contentType:'text/event-stream',body:'data: {"inventory":false,"metrics":false}\n\n'});
    if(path==='/api/projects') return route.fulfill({json:{collectedAt:'2026-10-08T00:00:00Z',projects:[project]}});
    if(path==='/api/jobs') return route.fulfill({json:history});
    if(path==='/api/recreate/preview') {
      previews++;
      if(unsupported) return route.fulfill({status:400,json:{error:'configuration cannot be safely recreated: legacy links require manual management'}});
      return route.fulfill({json:{fingerprint:String(previews).padStart(64,'0'),order:['sample-db-1','sample-web-1'],items:project.containers.map(item=>({id:item.id,name:item.name,service:item.service,image:item.image,imageID:'sha256:old',running:true,preserved:['ports','mounts and data','networks and aliases','environment and labels','resource and security settings'],environment:['TOKEN']}))}});
    }
    if(path==='/api/recreate/submit') {
      submits++;
      const body=request.postDataJSON();
      if(body.id!==project.id || !body.confirm || body.fingerprint!==String(previews).padStart(64,'0')) throw Error('wrong submitted preview');
      if(stale) { stale=false; return route.fulfill({status:409,json:{error:'Target changed. Review a fresh preview.'}}); }
      history=[{id:'c4-job',projectName:'',targetID:project.id,domain:'engine',operation:body.operation,status:'succeeded',stage:'completed',outcome:'verified',createdAt:1791432000,updatedAt:1791432001,targetImages:[{previousContainerID:'a'.repeat(64),containerID:'c'.repeat(64),service:'db',imageID:'sha256:new',outcome:'verified',state:'running'}]}];
      return route.fulfill({status:202,json:history[0]});
    }
    if(path.endsWith('/remove/preview')) {
      const selected=url.searchParams.get('removeVolumes')==='true';
      return route.fulfill({json:{fingerprint:(selected?'e':'d').repeat(64),items:[{kind:'container',id:'a'.repeat(64),name:'sample-db-1',action:'remove'},
        {kind:'volume',id:'sample_data',name:'sample_data',action:selected?'remove':'keep',reason:selected?'':'Volume deletion was not selected.'},
        {kind:'volume',id:'shared_data',name:'shared_data',action:'keep',reason:'Also used by another container.'},
        {kind:'Compose file',id:'/srv/sample/compose.yml',name:'/srv/sample/compose.yml',action:'keep',reason:'Host source files remain.'}]}});
    }
    if(path.endsWith('/remove')) {
      removals++; lastRemoval=request.postDataJSON();
      if(!lastRemoval.confirm || !lastRemoval.removeVolumes || lastRemoval.fingerprint!=='e'.repeat(64)) throw Error('removal used stale volume choice');
      return route.fulfill({json:{items:[{kind:'volume',id:'sample_data',name:'sample_data',action:'remove',status:'removed'},
        {kind:'volume',id:'shared_data',name:'shared_data',action:'keep',status:'retained',reason:'Also used by another container.'}]}});
    }
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.setViewportSize({width:1440,height:1000});
  await page.goto('http://127.0.0.1:5173');
  await page.getByRole('button',{name:/Compose running sample/}).click();
  const drawer=page.getByRole('dialog',{name:'sample',exact:true});
  await drawer.getByText('No recorded operations.',{exact:true}).waitFor();
  const menu=drawer.getByLabel('More actions',{exact:true});
  await menu.click();
  await drawer.getByRole('button',{name:'Update project images',exact:true}).click();
  const update=page.getByRole('dialog',{name:'Update images?',exact:true});
  await update.getByRole('button',{name:'Confirm update',exact:true}).waitFor();
  if(!await update.getByRole('button',{name:'Cancel',exact:true}).evaluate(el=>document.activeElement===el)) throw Error('update did not receive Cancel focus');
  if(await update.innerText().then(text=>text.includes('private-secret'))) throw Error('preview leaked environment values');
  for(const width of [1440,768,390,320]) {
    await page.setViewportSize({width,height:1000});
    if(await update.evaluate(el=>el.scrollWidth>el.clientWidth+1)) throw Error('update dialog overflows '+width);
  }
  await page.screenshot({path:'output/playwright/c4-update-320.png'});
  await page.keyboard.press('Escape');
  await update.waitFor({state:'hidden'});
  if(submits!==0) throw Error('cancel submitted a replacement');
  await page.waitForFunction(()=>document.activeElement?.getAttribute('aria-label')==='More actions');
  if(!await menu.evaluate(el=>document.activeElement===el)) throw Error('cancel did not return focus');
  await menu.click(); await drawer.getByRole('button',{name:'Update project images',exact:true}).click();
  await update.getByRole('button',{name:'Confirm update',exact:true}).click();
  await update.getByRole('alert').filter({hasText:'Target changed.'}).waitFor();
  if(await update.getByRole('button',{name:'Confirm update',exact:true}).count()) throw Error('stale preview allowed resubmit');
  await update.getByRole('button',{name:'Review again',exact:true}).click();
  await update.getByRole('button',{name:'Confirm update',exact:true}).click();
  await update.waitFor({state:'hidden'});
  await drawer.getByText('update: Completed',{exact:true}).waitFor();
  await drawer.getByText('Recent operations (1)',{exact:true}).click();
  await drawer.getByRole('list',{name:'Container outcomes',exact:true}).waitFor();
  unsupported=true;
  await menu.click(); await drawer.getByRole('button',{name:'Recreate project containers',exact:true}).click();
  const recreate=page.getByRole('dialog',{name:'Recreate containers?',exact:true});
  await recreate.getByRole('alert').filter({hasText:'legacy links'}).waitFor();
  if(await recreate.getByRole('button',{name:'Confirm recreate',exact:true}).count()) throw Error('unsupported target can be recreated');
  await recreate.getByRole('button',{name:'Cancel',exact:true}).click();
  await menu.click(); await drawer.getByRole('button',{name:'Remove project',exact:true}).click();
  const removal=page.getByRole('dialog',{name:'Remove project?',exact:true});
  const volumes=removal.getByRole('checkbox',{name:'Delete exclusive project volumes',exact:true});
  await removal.getByText('Volume deletion was not selected.',{exact:true}).waitFor();
  if(await volumes.isChecked()) throw Error('volume deletion enabled by default');
  await volumes.focus(); await page.keyboard.press('Space');
  const owned=removal.getByRole('listitem').filter({hasText:'sample_data'});
  await owned.getByText('will remove',{exact:true}).waitFor();
  await removal.getByRole('listitem').filter({hasText:'shared_data'}).getByText('will keep',{exact:true}).waitFor();
  for(const width of [1440,768,390,320]) {
    await page.setViewportSize({width,height:1000});
    if(await removal.evaluate(el=>el.scrollWidth>el.clientWidth+1)) throw Error('removal dialog overflows '+width);
  }
  await page.screenshot({path:'output/playwright/c4-remove-320.png'});
  await removal.getByRole('button',{name:'Remove project',exact:true}).click();
  const report=page.getByRole('dialog',{name:'Removal report',exact:true});
  await report.getByText(/Selected resources removed/).waitFor();
  if(removals!==1 || !lastRemoval.removeVolumes) throw Error('removal choice not submitted');
  await report.getByRole('button',{name:'Close report',exact:true}).click();
  console.log('PASS: C4 preview/cancel/focus, stale retry, unsupported rejection, container outcomes, explicit volumes/shared protection and 1440/768/390/320px');
}
