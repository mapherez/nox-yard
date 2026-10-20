async (page) => {
  let settings = {revision:1,mcpEnabled:false,apiEnabled:false,authMode:'token',write:false,secrets:false,autoApply:false,hasToken:false,tokenCreatedAt:0,network:{yard:{bind:'127.0.0.1',port:8095},mcp:{bind:'127.0.0.1',port:8096}}};
  const applied=structuredClone(settings.network); let generations=0,applies=0,historyReads=0;
  const history=[];
  await page.route('**/api/**',async route=>{
    const request=route.request(),path=new URL(request.url()).pathname,method=request.method();
    if(path==='/api/bootstrap')return route.fulfill({json:{needsSetup:false,authenticated:true,username:'owner',csrfToken:'fixture-csrf'}});
    if(path==='/api/projects')return route.fulfill({json:{collectedAt:new Date().toISOString(),projects:[]}});
    if(path==='/api/projects/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
    if(path==='/api/machine'){
      if(method==='PUT'){const body=request.postDataJSON();if(request.headers()['x-csrf-token']!=='fixture-csrf')throw Error('missing CSRF');settings={...body,revision:settings.revision+1,hasToken:settings.hasToken,tokenCreatedAt:settings.tokenCreatedAt};return route.fulfill({json:{settings}});}
      return route.fulfill({json:{settings,operational:settings.mcpEnabled,networkStatus:{available:true,pending:JSON.stringify(applied)!==JSON.stringify(settings.network),applied}}});
    }
    if(path==='/api/machine/token'){
      const body=request.postDataJSON();if(!body.confirm||request.headers()['x-csrf-token']!=='fixture-csrf')throw Error('unconfirmed token change');
      settings.hasToken=!body.revoke;settings.tokenCreatedAt=Math.floor(Date.now()/1000);generations++;
      history.unshift({id:generations,action:body.revoke?'revoked':generations===1?'generated':'replaced',actor:'owner',createdAt:settings.tokenCreatedAt});
      return route.fulfill({json:{token:body.revoke?'':'yard_fixture_secret_'+generations}});
    }
    if(path==='/api/machine/token/history'){historyReads++;return route.fulfill({json:history.slice(0,10)});}
    if(path==='/api/machine/apply'){if(!request.postDataJSON().confirm)throw Error('unconfirmed recreation');applies++;return route.fulfill({json:{id:'fixture-network',status:'running'}});}
    if(path==='/api/managed/settings')return route.fulfill({json:{projectsBase:'/srv/projects'}});
    if(path==='/api/self-update')return route.fulfill({json:{status:'up_to_date',automatic:false,intervalMinutes:15}});
    if(path==='/api/metrics')return route.fulfill({json:{}});
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.setViewportSize({width:1440,height:1000});await page.goto('http://127.0.0.1:5173');await page.evaluate(()=>document.fonts.ready);
  await page.getByRole('button',{name:'MCP Connection: MCP disabled',exact:true}).click();
  const drawer=page.getByRole('dialog',{name:'Settings',exact:true});await drawer.waitFor();
  await drawer.getByRole('heading',{name:'MCP & access'}).waitFor();
  if(historyReads)throw Error('history fetched before requested');
  await drawer.getByRole('button',{name:'Generate new token',exact:true}).click();
  const confirm=page.getByRole('dialog',{name:'Generate a new Yard token?',exact:true});await confirm.waitFor();
  await confirm.getByRole('button',{name:'Cancel',exact:true}).click();if(generations)throw Error('cancel rotated token');
  if(!await drawer.isVisible())throw Error('nested modal closed settings');
  await drawer.getByRole('button',{name:'Generate new token',exact:true}).click();
  await confirm.getByRole('button',{name:'Generate token',exact:true}).click();
  const issued=page.getByRole('dialog',{name:'Your new Yard token',exact:true});await issued.waitFor();
  if(await issued.getByLabel('New Yard token').inputValue()!=='yard_fixture_secret_1')throw Error('new token missing');
  await issued.getByRole('button',{name:'Close',exact:true}).click();
  if(await page.locator('input[value="yard_fixture_secret_1"]').count())throw Error('token retained after dismissal');
  await drawer.getByRole('button',{name:'History',exact:true}).click();
  const historyDialog=page.getByRole('dialog',{name:'Token history',exact:true});await historyDialog.getByText('generated',{exact:true}).waitFor();
  if(historyReads!==1||await historyDialog.getByText(/yard_fixture_secret/).count())throw Error('history leaks token or polls');
  await page.keyboard.press('Escape');if(!await drawer.isVisible())throw Error('Escape closed parent drawer');
  await drawer.getByLabel('Enable MCP',{exact:true}).check();await drawer.getByRole('button',{name:'Save access settings',exact:true}).click();
  await drawer.getByText('Operational',{exact:true}).waitFor();
  await drawer.getByLabel('MCP authentication',{exact:true}).selectOption('anonymous');await drawer.getByText(/Anyone who can reach/).waitFor();
  await drawer.getByLabel('Allow writes:',{exact:false}).check();await drawer.getByRole('button',{name:'Save access settings',exact:true}).click();
  await drawer.getByRole('button',{name:'Save access settings',exact:true}).waitFor({state:'visible'});
  const mcp=drawer.getByRole('group',{name:'MCP',exact:true});
  await mcp.getByLabel('Port',{exact:true}).fill('18096');await drawer.getByRole('button',{name:'Save access settings',exact:true}).click();
  await drawer.getByText('Pending network changes',{exact:true}).waitFor();
  if(applies)throw Error('saving recreated without opt-in');
  await drawer.getByRole('button',{name:'Apply now',exact:true}).click();const apply=page.getByRole('dialog',{name:'Apply network changes?',exact:true});await apply.waitFor();
  await apply.getByRole('button',{name:'Apply changes',exact:true}).click();await drawer.getByText(/Network recreation started/).waitFor();if(applies!==1)throw Error('apply not submitted once');
  for(const width of [1920,1280,1024,768,390,320]){
    await page.setViewportSize({width,height:1000});
    const overflow=await drawer.evaluate(el=>el.scrollWidth>el.clientWidth+1);if(overflow)throw Error('settings overflow at '+width);
  }
  await page.setViewportSize({width:1440,height:1000});await drawer.getByRole('button',{name:'Close settings',exact:true}).click();
  await page.getByRole('button',{name:'Hide sidebar',exact:true}).click();
  const indicator=page.getByRole('button',{name:'MCP Connection: MCP operational',exact:true});const box=await indicator.boundingBox();
  if(!box||box.width>60||box.height>60||!await indicator.getByText('MCP',{exact:true}).isVisible())throw Error('compact indicator too large');
  await indicator.click();await drawer.getByRole('heading',{name:'MCP & access'}).waitFor();
  await drawer.getByRole('button',{name:'History',exact:true}).click();await historyDialog.waitFor();
  await page.screenshot({path:'output/playwright/machine-history.png',fullPage:true});await page.keyboard.press('Escape');
  await page.screenshot({path:'output/playwright/machine-settings.png',fullPage:true});
  console.log('PASS: machine settings, token rotation/history, scopes, pending apply, responsive drawer and sidebar indicator');
}
