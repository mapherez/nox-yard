async (page) => {
  await page.unrouteAll({behavior:'wait'});
  const id='a'.repeat(64);
  let unauthorized=false;
  await page.addInitScript(() => {
    window.fixtureStreams=[];window.fixtureSockets=[];
    class FixtureEvents extends EventTarget {
      static OPEN=1;static CLOSED=2;
      constructor(url){super();this.url=url;this.readyState=1;window.fixtureStreams.push(this);setTimeout(()=>this.onopen?.(),0)}
      close(){this.readyState=2}
    }
    class FixtureSocket {
      static OPEN=1;
      constructor(url){this.url=url;this.readyState=1;window.fixtureSockets.push(this);setTimeout(()=>this.onopen?.(),0)}
      send(data){if(typeof data==='string'&&JSON.parse(data).type==='open')setTimeout(()=>this.onmessage?.({data:JSON.stringify({type:'ready'})}),0)}
      close(){this.readyState=3}
    }
    window.EventSource=FixtureEvents;window.WebSocket=FixtureSocket;
  });
  await page.route('**/api/**',route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/bootstrap')return route.fulfill({json:{authenticated:true,needsSetup:false,username:'owner',csrfToken:'fixture'}});
    if(path==='/api/login')return route.fulfill({status:401,json:{error:'Invalid username or password.'}});
    if(path==='/api/projects')return route.fulfill(unauthorized?{status:401,json:{error:'Sign in to continue.'}}:{json:{collectedAt:'2026-10-08T00:00:00Z',projects:[{id:'compose:sample',name:'sample',kind:'managed-compose',state:'running',health:'healthy',cpuPercent:null,memoryBytes:null,networkRxBytes:null,networkTxBytes:null,uptimeSeconds:null,containers:[{id,name:'demo',service:'demo',image:'alpine',state:'running',health:'healthy',terminalAvailable:true,cpuPercent:null,memoryBytes:null,networkRxBytes:null,networkTxBytes:null,uptimeSeconds:null}]}]}});
    if(path.endsWith('/schedule'))return route.fulfill({json:{enabled:false,targetID:'compose:sample',timezone:'Etc/UTC',time:'03:00',eligible:true}});
    if(path==='/api/jobs')return route.fulfill({json:[]});
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});
  const card=page.getByRole('button',{name:/Managed Compose running sample/});
  for(const kind of ['inventory','logs','terminal','http']){
    unauthorized=false;await page.goto('http://127.0.0.1:5173');await card.waitFor();
    if(kind==='logs'||kind==='terminal'){
      await card.click();await page.getByRole('tab',{name:kind==='logs'?'Logs':'Terminal',exact:true}).click();
      await page.getByText(kind==='logs'?'Live':'Connected',{exact:true}).waitFor();
    }
    if(kind==='http'){
      unauthorized=true;await page.getByRole('button',{name:'Refresh projects',exact:true}).click();
    }else{
      await page.evaluate(kind=>{
        if(kind==='terminal'){const socket=window.fixtureSockets.at(-1);socket.readyState=3;socket.onclose({code:4001});return}
        const source=window.fixtureStreams.find(s=>kind==='inventory'?s.url.endsWith('/events'):s.url.endsWith('/logs'));
        if(!source)throw Error('Requested stream never opened');source.dispatchEvent(new Event('session-expired'));
      },kind);
    }
    await page.getByRole('heading',{name:'Sign in to NoX Yard',exact:true}).waitFor();
    if(await page.getByRole('dialog').count())throw Error('Protected drawer remains after expiry');
    if(!await page.evaluate(()=>window.fixtureStreams.every(s=>s.readyState===2)&&window.fixtureSockets.every(s=>s.readyState===3)))throw Error('Session expiry did not release streams');
  }
  await page.getByLabel('Username', {exact:true}).fill('owner');await page.getByLabel('Password',{exact:true}).fill('wrong-password');await page.getByRole('button',{name:'Sign in',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'Invalid username or password.'}).waitFor();
  if(await page.getByLabel('Username',{exact:true}).inputValue()!=='owner')throw Error('Failed login reset form');
  for(const width of [1440,768,390,320]){await page.setViewportSize({width,height:1000});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Login overflow '+width)}
  console.log('PASS: inventory/log/terminal/HTTP expiry returns to login, closes streams and drawers, failed login retains input, responsive sign-in');
}
