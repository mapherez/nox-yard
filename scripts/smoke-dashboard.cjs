/* Dashboard interaction regressions against API fixtures; no Docker mutations. */
async (page) => {
  await page.unrouteAll({behavior:'wait'});
  await page.addInitScript(() => {
    window.dashboardStreams=[];
    class Events extends EventTarget {
      static OPEN=1;
      constructor(){super();this.readyState=1;window.dashboardStreams.push(this)}
      close(){this.readyState=2}
    }
    window.EventSource=Events;
  });
  const names=['adguard','grafana-alloy','home-assistant','metro-motion','nginx-proxy-manager','node-exporter','nox-backend','nox-yard','z-exporter'];
  let projects=names.map((name,index)=>({
    id:`compose:${name}`,name,kind:index===3?'managed-compose':index===6?'standalone':'external-compose',state:index===8?'stopped':'running',health:index===8?'none':'healthy',cpuPercent:index/10,memoryBytes:16*1024*1024,uptimeSeconds:100000,networkRxBytes:0,networkTxBytes:0,
    containers:[{id:`container-${index}`,name:index===2?'smart-home-web':`${name}-1`,service:index===2?'automation-service':'app',image:'fixture:latest',state:'running',health:'healthy',terminalAvailable:true,cpuPercent:0.2,memoryBytes:16*1024*1024,uptimeSeconds:100000,networkRxBytes:0,networkTxBytes:0}],
  }));
  const errors=[]; const onError=error=>errors.push(error.message); page.on('pageerror',onError);
  await page.route('**/api/**',route=>{
    const pathname=new URL(route.request().url()).pathname;
    if(pathname==='/api/bootstrap') return route.fulfill({json:{authenticated:true,needsSetup:false,username:'dashboard-fixture',csrfToken:'fixture'}});
    if(pathname==='/api/projects') return route.fulfill({json:{collectedAt:'2026-10-20T10:00:00Z',projects}});
    if(pathname==='/api/configuration-migration') return route.fulfill({json:[]});
    if(pathname==='/api/jobs') return route.fulfill({json:[]});
    if(pathname.endsWith('/schedule')) return route.fulfill({json:{enabled:false,targetID:'compose:adguard',timezone:'Etc/UTC',time:'03:00',eligible:true}});
    if(pathname.includes('/configuration/')) return route.fulfill({json:{available:false,state:'ready',files:[],variables:[]}});
    return route.fulfill({json:{}});
  });
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.setViewportSize({width:1440,height:1100});
  await page.goto('http://127.0.0.1:5173');
  await page.evaluate(()=>localStorage.removeItem('nox-yard-project-display:dashboard-fixture'));
  await page.reload();
  const collection=page.getByRole('region',{name:'Docker projects',exact:true});
  const items=()=>collection.locator('[data-project-id]');
  const item=name=>collection.locator(`[data-project-id="compose:${name}"]`);
  const handle=name=>item(name).locator('[data-drag-handle]');
  const search=page.getByRole('searchbox',{name:'Search projects and containers'});
  const expectOrder=async expected=>{
    await page.waitForFunction(expected=>{
      const ids=Array.from(document.querySelectorAll('[data-project-id]')).map(el=>el.dataset.projectId.replace('compose:',''));
      return JSON.stringify(ids)===JSON.stringify(expected);
    },expected);
  };
  const sort=async descending=>{
    await page.getByRole('button',{name:'Sort projects',exact:true}).click();
    await page.getByRole('menuitemradio',{name:descending?'Name: Z to A':'Name: A to Z'}).click();
    await expectOrder(descending?[...names].reverse():names);
  };
  await expectOrder(names);
  // A container or Compose service match exposes its parent, never a flattened card.
  for(const value of ['SMART-HOME','automation-service',' home ASSISTANT ']){
    await search.fill(value); await expectOrder(['home-assistant']);
    if(!await item('home-assistant').getByText('CPU',{exact:true}).count())throw Error('Search lost project metrics');
  }
  await search.fill('no-such-project');
  await page.getByRole('heading',{name:'No matching projects'}).waitFor();
  if(await page.getByText('No containers found',{exact:true}).count())throw Error('Search incorrectly reports an empty Docker host');
  await page.getByRole('button',{name:'Clear search',exact:true}).first().click(); await expectOrder(names);
  const searchBox=await search.boundingBox(); const headingBox=await page.getByRole('heading',{name:'Projects',exact:true}).boundingBox();
  if(searchBox.y>=headingBox.y)throw Error('Desktop search must precede dashboard header');
  const mainBounds=await page.locator('main#content').evaluate(el=>{const r=el.getBoundingClientRect(),s=getComputedStyle(el);return {width:r.width-parseFloat(s.paddingLeft)-parseFloat(s.paddingRight)}});
  if(Math.abs(await page.getByRole('search').evaluate(el=>el.getBoundingClientRect().width)-mainBounds.width)>1)throw Error('Search does not fill main content');

  await page.getByRole('button',{name:'List view',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="Docker projects"]').dataset.layout==='list');
  const listHeight=(await item('adguard').boundingBox()).height;
  if(listHeight>48)throw Error('List must use compact table rows, got '+listHeight+'px');
  for(const text of ['Compose','running','adguard','1','healthy','0.0%','16.0 MiB','1d 3h']){
    if(!await item('adguard').getByText(text,{exact:true}).count())throw Error('List lost '+text);
  }
  if(await collection.getByRole('columnheader').count()!==9)throw Error('Project table lost its column headers');
  // The sort menu works entirely from the keyboard and restores focus.
  const sortButton=page.getByRole('button',{name:'Sort projects',exact:true});
  await sortButton.focus(); await page.keyboard.press('ArrowDown'); await page.keyboard.press('End'); await page.keyboard.press('Enter');
  await expectOrder([...names].reverse());
  if(!await sortButton.evaluate(el=>el===document.activeElement))throw Error('Sort did not restore focus');
  await page.reload(); await expectOrder([...names].reverse());
  if(await collection.getAttribute('data-layout')!=='list')throw Error('View preference did not persist');
  await page.screenshot({path:'output/playwright/dashboard-list-desktop.png'});
  await sort(false);
  await page.getByRole('button',{name:'Grid view',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="Docker projects"]').dataset.layout==='grid');
  if((await item('adguard').boundingBox()).height<=listHeight)throw Error('Desktop list is not more compact than grid');
  await page.emulateMedia({reducedMotion:'no-preference'});

  const dragTo=async(source,target,finish=true)=>{
    await handle(source).scrollIntoViewIfNeeded();
    const from=await handle(source).boundingBox(), to=await item(target).boundingBox(), bounds=await collection.boundingBox();
    await page.mouse.move(from.x+from.width/2,from.y+from.height/2);await page.mouse.down();
    await page.mouse.move(Math.max(bounds.x+48,Math.min(to.x+to.width/2,bounds.x+bounds.width-48)),to.y+to.height/2,{steps:12});
    await page.locator('[data-drag-ghost]').waitFor();
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Drag ghost caused page overflow');
    if(finish)await page.mouse.up();
  };
  const moved=['adguard','node-exporter','grafana-alloy','home-assistant','metro-motion','nginx-proxy-manager','nox-backend','nox-yard','z-exporter'];
  await dragTo('node-exporter','grafana-alloy',false);
  await expectOrder(moved);
  if(await collection.locator('[data-drop-target="true"]').getAttribute('data-project-id')!=='compose:node-exporter')throw Error('Drop marker does not occupy the future slot');
  await page.screenshot({path:'output/playwright/dashboard-drag-grid.png'});
  await page.mouse.up(); await page.locator('[data-drag-ghost]').waitFor({state:'hidden'});await expectOrder(moved);
  if(await page.locator('dialog[open]').count())throw Error('Dragging opened project details');
  await page.reload();await expectOrder(moved);
  await page.getByRole('button',{name:'List view',exact:true}).click(); await expectOrder(moved);
  await dragTo('node-exporter','nox-yard',false);
  if((await collection.locator('[data-drop-target="true"]').boundingBox()).height>48)throw Error('Table drop marker expanded the row');
  await page.waitForTimeout(220);
  await page.screenshot({path:'output/playwright/dashboard-drag-table.png'});
  await page.mouse.up();
  const movedList=['adguard','grafana-alloy','home-assistant','metro-motion','nginx-proxy-manager','nox-backend','nox-yard','node-exporter','z-exporter'];
  await expectOrder(movedList);
  await dragTo('node-exporter','grafana-alloy',false);await page.keyboard.press('Escape');await page.mouse.up();await expectOrder(movedList);
  await dragTo('node-exporter','grafana-alloy',false);await page.mouse.move(4,200);await page.mouse.up();await expectOrder(movedList);
  await page.setViewportSize({width:1440,height:600});await page.evaluate(()=>scrollTo(0,0));
  const scrollHandle=await handle('adguard').boundingBox();
  await page.mouse.move(scrollHandle.x+scrollHandle.width/2,scrollHandle.y+scrollHandle.height/2);await page.mouse.down();
  await page.mouse.move(scrollHandle.x+80,592,{steps:10});
  await page.waitForFunction(()=>scrollY>40);await page.keyboard.press('Escape');await page.mouse.up();await expectOrder(movedList);
  await page.setViewportSize({width:1440,height:1100});await page.evaluate(()=>scrollTo(0,0));
  // Keyboard and filtered reordering preserve every hidden project's slot.
  await sort(false);await search.fill('exporter');await expectOrder(['node-exporter','z-exporter']);
  await handle('z-exporter').focus();await page.keyboard.press('Home');await expectOrder(['z-exporter','node-exporter']);
  if(!await handle('z-exporter').evaluate(el=>el===document.activeElement))throw Error('Keyboard reordering lost focus');
  await search.fill('');await expectOrder(['adguard','grafana-alloy','home-assistant','metro-motion','nginx-proxy-manager','z-exporter','nox-backend','nox-yard','node-exporter']);
  await sort(false);

  // Live inventory invalidates a pending drag instead of persisting stale IDs.
  await dragTo('node-exporter','grafana-alloy',false);
  projects=projects.filter(project=>project.name!=='node-exporter');
  await page.evaluate(()=>window.dashboardStreams.at(-1).dispatchEvent(new MessageEvent('change',{data:JSON.stringify({inventory:true})})));
  await page.locator('[data-drag-ghost]').waitFor({state:'hidden'});await page.mouse.up();
  await expectOrder(names.filter(name=>name!=='node-exporter'));
  projects=names.map(name=>projects.find(project=>project.name===name)).filter(Boolean);

  // Validate both layouts at narrow tablet/mobile sizes and capture review artifacts.
  for(const layout of ['grid','list']){
    await page.getByRole('button',{name:layout==='grid'?'Grid view':'List view',exact:true}).click();
    for(const width of [1440,1024,768,390,320]){
      await page.setViewportSize({width,height:1000});await page.evaluate(()=>scrollTo(0,0));
      if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error(`${layout} overflows at ${width}px`);
      if(await items().evaluateAll(items=>items.some(el=>el.scrollWidth>el.clientWidth+1)))throw Error(`${layout} item overflows at ${width}px`);
      const truncated=await items().evaluateAll(items=>items.flatMap(el=>Array.from(el.querySelectorAll('strong')).filter(value=>value.scrollWidth>value.clientWidth+1).map(value=>({text:value.textContent,needed:value.scrollWidth,available:value.clientWidth}))));
      if(truncated.length)throw Error(`${layout} metric values truncated at ${width}px: ${JSON.stringify(truncated)}`);
      if(layout==='list'){
        const heights=await items().evaluateAll(items=>items.map(el=>el.getBoundingClientRect().height));
        if(heights.some(height=>height>48))throw Error('Table stacked or expanded rows at '+width+'px');
        if(width<1100&&!await collection.evaluate(el=>el.scrollWidth>el.clientWidth))throw Error('Narrow table must scroll horizontally at '+width+'px');
      }
      const s=await search.boundingBox(),h=await page.getByRole('heading',{name:'Projects',exact:true}).boundingBox();
      if(width<768?s.y<=h.y:s.y>=h.y)throw Error(`Search order incorrect at ${width}px`);
      await page.screenshot({path:`output/playwright/dashboard-${layout}-${width}.png`});
      if(layout==='list'){
        await collection.evaluate(el=>{el.scrollLeft=el.scrollWidth});
        const viewport=await collection.boundingBox(),last=await item('adguard').locator('td').last().boundingBox();
        if(last.x<viewport.x+40-1||last.x+last.width>viewport.x+viewport.width+1)throw Error('Final table column is inaccessible at '+width+'px');
        if(width===320){
          await page.screenshot({path:'output/playwright/dashboard-table-scroll-320.png'});
          await dragTo('adguard','grafana-alloy',false);
          if(!await collection.locator('[data-drop-target="true"]').count())throw Error('Horizontal scroll broke table drop targeting');
          await page.keyboard.press('Escape');await page.mouse.up();
        }
        await collection.evaluate(el=>{el.scrollLeft=0});
      }
    }
  }
  // Use actual Chromium touch input, not synthetic pointer handlers.
  await page.setViewportSize({width:390,height:1000});
  await page.evaluate(()=>scrollTo(0,0));
  await collection.evaluate(el=>{el.scrollLeft=el.scrollWidth});
  const touch=await page.context().newCDPSession(page);
  const from=await handle('adguard').boundingBox(),to=await handle('grafana-alloy').boundingBox();
  const point=(x,y)=>[{x,y,id:1}];
  await touch.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:point(from.x+from.width/2,from.y+from.height/2)});
  await touch.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:point(to.x+to.width/2,to.y+to.height/2)});
  await page.locator('[data-drag-ghost]').waitFor();
  await touch.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
  const touchOrder=['grafana-alloy','adguard',...names.slice(2).filter(name=>name!=='node-exporter')];
  await expectOrder(touchOrder);
  const cancelFrom=await handle('adguard').boundingBox(),cancelTo=await handle('home-assistant').boundingBox();
  await touch.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:point(cancelFrom.x+cancelFrom.width/2,cancelFrom.y+cancelFrom.height/2)});
  await touch.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:point(cancelTo.x+cancelTo.width/2,cancelTo.y+cancelTo.height/2)});
  await page.locator('[data-drag-ghost]').waitFor();
  const metrics=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/metrics');
  await page.evaluate(()=>window.dashboardStreams.at(-1).dispatchEvent(new MessageEvent('change',{data:JSON.stringify({metrics:true})})));
  await metrics;
  if(!await page.locator('[data-drag-ghost]').count())throw Error('Metric update canceled a valid gesture');
  await touch.send('Input.dispatchTouchEvent',{type:'touchCancel',touchPoints:[]});
  await page.locator('[data-drag-ghost]').waitFor({state:'hidden'});await expectOrder(touchOrder);
  await touch.detach();
  // Releasing a card still opens the normal project drawer.
  await item('adguard').locator('button[aria-haspopup="dialog"]').click();
  await page.getByRole('dialog',{name:'adguard',exact:true}).waitFor();await page.keyboard.press('Escape');
  // Blocked storage cannot prevent loading, changing view or ordering projects.
  await page.addInitScript(()=>{
    Storage.prototype.getItem=function(){throw new DOMException('Storage disabled','SecurityError')};
    Storage.prototype.setItem=function(){throw new DOMException('Storage disabled','SecurityError')};
  });
  await page.reload();await expectOrder(names.filter(name=>name!=='node-exporter'));
  await page.getByRole('button',{name:'List view',exact:true}).click();
  await handle('grafana-alloy').focus();await page.keyboard.press('Home');
  await expectOrder(['grafana-alloy','adguard',...names.slice(2).filter(name=>name!=='node-exporter')]);
  if(errors.length)throw Error('Dashboard browser errors: '+errors.join('; '));
  page.off('pageerror',onError);
  console.log('PASS: grid/table parity, compact rows and headers, contained horizontal scroll, live grouped search, persisted views/sorts/orders, mouse/touch/keyboard reordering including scrolled tables, ghost/preview, cancel, filtered slots, live removal and 1440/1024/768/390/320px');
}
