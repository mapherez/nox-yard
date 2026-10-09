/* Run with playwright-cli run-code --filename scripts/smoke-schedules.cjs; local Vite required. */
async (page) => {
  await page.unrouteAll({behavior:'wait'});
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  const target='compose:sample';let status={targetID:target,enabled:false,time:'03:00',timezone:'Europe/Lisbon',eligible:true};let writes=0;let failSave=true;let failRead=false;
  await page.route('**/api/**',route=>{
    const request=route.request(),url=new URL(request.url()),path=url.pathname;
    if(path==='/api/bootstrap') return route.fulfill({json:{authenticated:true,needsSetup:false,username:'admin',csrfToken:'fixture'}});
    if(path==='/api/projects/events') return route.fulfill({contentType:'text/event-stream',body:'data: {"inventory":false,"metrics":false}\n\n'});
    if(path==='/api/projects') return route.fulfill({json:{collectedAt:'2026-10-08T00:00:00Z',projects:[{id:target,name:'sample',kind:'managed-compose',state:'running',health:'healthy',containers:[],cpuPercent:null,memoryBytes:null,uptimeSeconds:null}]}});
    if(path==='/api/jobs') return route.fulfill({json:status.lastJobID?[{id:status.lastJobID,targetID:target,projectName:'sample',domain:'schedule',operation:'update',status:'succeeded',stage:'completed',outcome:'skipped',scheduledFor:1791432000,createdAt:1791432000,updatedAt:1791432000,error:status.lastReason}]:[]});
    if(path.endsWith('/schedule')){
      if(request.method()==='PUT'){
        if(request.headers()['x-csrf-token']!=='fixture')throw Error('schedule save lost CSRF');
        const body=request.postDataJSON();if(typeof body.enabled!=='boolean'||!/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(body.time)||Object.keys(body).length!==2)throw Error('wrong schedule payload');writes++;
        if(failSave){failSave=false;return route.fulfill({status:409,json:{error:'Project state changed; review before enabling.'}})}
        const [hour,minute]=body.time.split(':').map(Number);
        status={...status,enabled:body.enabled,time:body.time,nextAt:body.enabled?Date.UTC(2026,9,9,hour-1,minute)/1000:0};return route.fulfill({json:status});
      }
      return route.fulfill(failRead?{status:503,json:{error:'Schedule settings unavailable.'}}:{json:status});
    }
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});await page.setViewportSize({width:1440,height:1000});await page.goto('http://127.0.0.1:5173');
  const card=page.getByRole('button',{name:/Managed Compose running sample/});await card.click();
  const drawer=page.getByRole('dialog',{name:'sample',exact:true}),toggle=drawer.getByRole('checkbox',{name:'Enable automatic updates for this project'}),save=drawer.getByRole('button',{name:'Save automatic updates'}),time=drawer.getByLabel('Daily check time (Europe/Lisbon)');
  await toggle.waitFor();await drawer.getByText(/Daily at 03:00 \(Europe\/Lisbon, server timezone\)/).waitFor();
  if(await toggle.isChecked()||!await save.isDisabled()||writes!==0)throw Error('default setting or implicit save');
  await time.fill('06:45');if(writes!==0)throw Error('time edit saved implicitly');
  await toggle.focus();await page.keyboard.press('Space');if(await save.isDisabled()||writes!==0)throw Error('checkbox not explicit draft');
  await save.click();await drawer.getByRole('alert').filter({hasText:'Project state changed'}).waitFor();if(!await toggle.isChecked()||await save.isDisabled()||await time.inputValue()!=='06:45')throw Error('failed save lost draft/retry');
  await save.click();await drawer.getByRole('status').filter({hasText:'Automatic update settings saved.'}).waitFor();if(!await toggle.isChecked()||!await save.isDisabled())throw Error('saved state wrong');
  await page.keyboard.press('Escape');await card.click();await toggle.waitFor();if(!await toggle.isChecked())throw Error('drawer reopen lost opt-in');
  await page.reload();await card.click();await toggle.waitFor();if(!await toggle.isChecked()||await time.inputValue()!=='06:45')throw Error('browser reload lost schedule');
  await time.fill('');await save.click();if(writes!==2)throw Error('empty time bypassed native validation');
  await time.fill('21:15');if(await save.isDisabled())throw Error('time-only edit cannot be saved');await save.click();await drawer.getByRole('status').filter({hasText:'Automatic update settings saved.'}).waitFor();if(status.time!=='21:15'||!status.enabled)throw Error('time-only save lost enabled state');
  await drawer.getByText(/Daily at 21:15 \(Europe\/Lisbon, server timezone\)/).waitFor();
  status={...status,lastAt:1791432000,lastOutcome:'skipped',lastJobID:'scheduled-fixture',lastReason:'The same image set previously failed. Waiting for new images or an explicit manual retry.'};
  await page.keyboard.press('Escape');await card.click();await drawer.getByText('Automatic update: Automatic check skipped',{exact:true}).waitFor();
  for(const width of [1440,768,390,320]){await page.setViewportSize({width,height:1000});if(await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('schedule drawer overflow '+width)}
  await page.screenshot({path:'output/playwright/c5-schedule-320.png'});
  // Disabling remains available even when the target becomes unavailable/protected.
  status={...status,eligible:false,reason:'Project availability could not be verified.'};await page.keyboard.press('Escape');await card.click();await toggle.waitFor();if(await toggle.isDisabled())throw Error('enabled unavailable schedule cannot be disabled');
  await toggle.focus();await page.keyboard.press('Space');await save.click();await drawer.getByRole('status').filter({hasText:'Automatic update settings saved.'}).waitFor();if(status.enabled||writes!==4||status.time!=='21:15')throw Error('disable not persisted');
  if(!await toggle.isDisabled())throw Error('unsupported opt-in not blocked');
  failRead=true;await page.keyboard.press('Escape');await card.click();await drawer.getByRole('alert').filter({hasText:'Schedule settings unavailable.'}).waitFor();await drawer.getByText('Automatic update: Automatic check skipped',{exact:true}).waitFor();failRead=false;
  await drawer.getByRole('button',{name:'Retry automatic update settings'}).click();await toggle.waitFor();
  if(errors.length)throw Error(errors.join(';'));
  console.log('PASS: opt-in/default-off, custom time/time-only save, keyboard draft/save, failure retry, timezone, reload, skipped history, unavailable disable and 1440/768/390/320 layouts');
}
