// Usage: node scripts/smoke.cjs <authenticated URL> [absolute playwright module path]
const {chromium}=require(process.argv[3]||'playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:'msedge'});
 try {
 const context=await browser.newContext({viewport:{width:1600,height:1100}});
 const page=await context.newPage();const errors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error')errors.push(m.text())});
 page.on('response',r=>{if(r.status()>=400)console.log('HTTP ERROR',r.status(),r.url())});
 await page.goto(process.argv[2]);await page.locator('.book').first().waitFor();
 await page.screenshot({path:'dist/library.png',fullPage:true});
 const books=await page.locator('.book .bookName').allTextContents();console.log('Library',books);
 for(const filename of [...books.filter(n=>n.endsWith('.pdf')),...books.filter(n=>!n.endsWith('.pdf'))]){
  await page.locator('#libraryBtn').click().catch(()=>{});
  if(await page.locator('#library').isHidden())await page.locator('#libraryBtn').click();
  await page.locator('.book').filter({has:page.locator('.bookName',{hasText:filename})}).click();
  await page.waitForFunction(name=>window.duoDebug?.ready&&window.duoDebug.name===name&&window.duoDebug.total>0,filename,{timeout:120000});
  const first=await page.evaluate(()=>window.duoDebug);console.log('OPEN',filename,first);
  await page.locator('#next').click();await page.waitForFunction(p=>window.duoDebug?.pair[0]===p+2,first.pair[0],{timeout:60000});
  await page.locator('#previous').click();await page.waitForFunction(p=>window.duoDebug?.pair[0]===p,first.pair[0]);
  await page.locator('#scrollBtn').click();await page.waitForFunction(()=>document.getElementById('scrollBtn').classList.contains('active'));
  const start=(await page.evaluate(()=>window.duoDebug)).pair[0];await page.locator('#next').click();await page.waitForFunction(p=>window.duoDebug?.pair[0]===p+1,start,{timeout:60000});
  await page.screenshot({path:'dist/'+first.format+'-reader.png'});
  await page.locator('#spreadBtn').click();
 }
 assert.deepEqual(errors,[]);
 // Two independent viewports must commit the same revision and adjacent pages.
 const left=await context.newPage(),right=await context.newPage();
 const cleanURL=process.argv[2].split('/?')[0];
 await left.goto(cleanURL+'/?side=left');await right.goto(cleanURL+'/?side=right');
 await left.waitForFunction(()=>window.duoDebug?.ready,{timeout:60000});await right.waitForFunction(()=>window.duoDebug?.ready,{timeout:60000});
 await left.locator('#next').click();
 await left.waitForTimeout(3000);
 const l=await left.evaluate(()=>window.duoDebug),r=await right.evaluate(()=>window.duoDebug);
 assert.equal(l.revision,r.revision);assert.equal(Math.abs(l.pair[0]-r.pair[0]),1);console.log('SYNC',l,r);
 console.log('PASS');
 } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exit(1)});
