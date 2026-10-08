/* Run with playwright-cli run-code --filename scripts/smoke-history.cjs. Requires local Vite. */
async (page) => {
  await (async (page) => {
  await page.unrouteAll({behavior:'wait'});
  await page.route('**/api/**', route => {
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/bootstrap') return route.fulfill({json:{authenticated:true,needsSetup:false,username:'admin',csrfToken:'fixture'}});
    if(path==='/api/projects/events') return route.fulfill({contentType:'text/event-stream',body:'data: {"inventory":false,"metrics":false}\n\n'});
    if(path==='/api/projects') return route.fulfill({json:{collectedAt:'2026-10-08T00:00:00Z',projects:[{id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'healthy',cpuPercent:null,memoryBytes:null,uptimeSeconds:null,networkRxBytes:null,networkTxBytes:null,containers:[]}]}});
    if(path==='/api/jobs') return route.fulfill({json:[{id:'job-fixture',projectName:'sample',targetID:'compose:sample',domain:'managed',operation:'update',status:'running',stage:'verifying',createdAt:1791432000,updatedAt:1791432001}]});
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.setViewportSize({width:1440,height:1000});
  await page.goto('http://127.0.0.1:5173');
}
)(page);
  await page.getByRole('button', {name:/Managed Compose running sample/}).click();
  await (async (page) => {
  const drawer=page.getByRole('dialog',{name:'sample',exact:true});
  const card=page.getByRole('button',{name:/Managed Compose running sample/});
  const restart=()=>drawer.getByRole('button',{name:'Restart sample',exact:true});
  await drawer.getByText('update: Verifying services',{exact:true}).waitFor();
  if(!await restart().isDisabled()) throw Error('Reloaded operation did not disable actions');
  await page.keyboard.press('Escape');
  await card.click();
  await drawer.getByText('update: Verifying services',{exact:true}).waitFor();
  await page.reload();
  await card.click();
  await drawer.getByText('update: Verifying services',{exact:true}).waitFor();
  if(!await restart().isDisabled()) throw Error('Browser reload lost active job');
  let current={id:'job-fixture',projectName:'sample',targetID:'compose:sample',domain:'managed',operation:'update',status:'failed',stage:'awaiting_recovery',outcome:'recovery_required',createdAt:1791432000,updatedAt:1791432002,error:'Worker exited before recording a verified result. Inspect the target, source files and retained resources on the host, then acknowledge recovery before retrying.',rollback:'not_performed',cleanupError:'Worker cleanup is pending.'};
  let failed=false;
  let ackCalls=0;
  await page.route('**/api/jobs?**',route=>route.fulfill(failed?{status:503,json:{error:'History temporarily unavailable.'}}:{json:[current]}));
  await page.route('**/api/jobs/job-fixture/recovery',route=>{
    const body=route.request().postDataJSON();
    if(body.confirm!==true||body.updatedAt!==current.updatedAt) throw Error('Acknowledgement did not use current review');
    ackCalls++;
    if(ackCalls===1) return route.fulfill({status:409,json:{error:'A worker is still active. Review again after it exits.'}});
    current={...current,stage:'completed',outcome:'recovery_acknowledged'};
    return route.fulfill({json:current});
  });
  await drawer.getByText('update: Recovery required',{exact:true}).waitFor();
  const acknowledge=drawer.getByRole('button',{name:'Acknowledge recovery',exact:true});
  if(!await acknowledge.isDisabled()||!await restart().isDisabled()) throw Error('Recovery does not require host review');
  const summary=drawer.locator('summary').filter({hasText:'Recent operations'});
  await summary.focus(); await page.keyboard.press('Space');
  await drawer.getByText('Rollback: not performed',{exact:true}).waitFor();
  await drawer.getByText('Cleanup: Worker cleanup is pending.',{exact:true}).waitFor();
  for(const width of [1440,768,390,320]) {
    await page.setViewportSize({width,height:1000});
    if(await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1)) throw Error('History overflows '+width);
  }
  await page.screenshot({path:'output/playwright/c2-recovery-320.png'});
  const reviewed=drawer.getByRole('checkbox',{name:'I inspected the target and retained resources on the host.'});
  await reviewed.focus(); await page.keyboard.press('Space');
  if(await acknowledge.isDisabled()) throw Error('Keyboard review did not enable acknowledgement');
  await acknowledge.click();
  await drawer.getByRole('alert').filter({hasText:'A worker is still active.'}).waitFor();
  if(!await restart().isDisabled()) throw Error('Failed acknowledgement released UI ownership');
  await acknowledge.click();
  await drawer.getByText('update: Recovery acknowledged',{exact:true}).waitFor();
  if(await restart().isDisabled()) throw Error('Successful acknowledgement did not release controls');
  if(!await drawer.getByRole('heading',{name:'Operations',exact:true}).evaluate(el=>document.activeElement===el)) throw Error('Recovery did not retain meaningful keyboard focus');
  failed=true;
  await drawer.getByRole('alert').filter({hasText:'History temporarily unavailable.'}).waitFor();
  if(!await restart().isDisabled()) throw Error('History failure admitted a conflicting action');
  failed=false;
  await drawer.getByRole('button',{name:'Retry',exact:true}).click();
  await drawer.getByText('update: Recovery acknowledged',{exact:true}).waitFor();
  await page.waitForFunction(()=>Array.from(document.querySelectorAll('#project-drawer button')).some(el=>el.getAttribute('aria-label')==='Restart sample'&&!el.disabled));
  if(await restart().isDisabled()) throw Error('Retry did not recover history');
  for(const [outcome,label] of [['unchanged','No image changes · containers preserved'],['cached','Images pulled · containers preserved'],['rolled_back','Failed · previous version restored']]) {
    current={...current,status:outcome==='rolled_back'?'failed':'succeeded',outcome,stage:'completed',error:'',rollback:outcome==='rolled_back'?'restored':'',cleanupError:''};
    await drawer.getByText(`update: ${label}`,{exact:true}).waitFor();
    if(await restart().isDisabled()) throw Error('Terminal C3 outcome kept actions blocked');
  }
  current={...current,status:'running',outcome:'',stage:'rolling_back'};
  await drawer.getByText('update: Restoring previous version',{exact:true}).waitFor();
  if(!await restart().isDisabled()) throw Error('Rollback did not keep actions blocked');
  current={...current,status:'failed',outcome:'rolled_back',stage:'completed',rollback:'restored'};
  await drawer.getByText('update: Failed · previous version restored',{exact:true}).waitFor();
  await drawer.getByLabel('More actions',{exact:true}).click();
  await drawer.getByRole('button',{name:'Update images',exact:true}).click();
  await drawer.getByText(/Uses the saved source\. Unchanged images preserve containers/).waitFor();
  if(await drawer.getByRole('button',{name:'Update images',exact:true}).isVisible()) throw Error('Maintenance menu covers update confirmation');
  if(!await drawer.getByRole('button',{name:'Cancel',exact:true}).evaluate(el=>document.activeElement===el)) throw Error('Confirmation did not receive keyboard focus');
  for(const width of [1440,768,390,320]) {
    await page.setViewportSize({width,height:1000});
    if(await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1)) throw Error('Update confirmation overflows '+width);
  }
  await page.screenshot({path:'output/playwright/c3-update-320.png'});
  await drawer.getByRole('button',{name:'Cancel',exact:true}).click();
  if(!await drawer.getByLabel('More actions',{exact:true}).evaluate(el=>document.activeElement===el)) throw Error('Cancel did not return focus to the maintenance menu');
  await page.keyboard.press('Escape');
  await drawer.waitFor({state:'hidden'});
  console.log('PASS: restored history/recovery, C3 unchanged/cache/rollback outcomes and action blocking, update confirmation and 1440/768/390/320px');
}
)(page);
}
