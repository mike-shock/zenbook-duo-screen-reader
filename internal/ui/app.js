import * as pdfjs from '/vendor/pdfjs/build/pdf.mjs';

pdfjs.GlobalWorkerOptions.workerSrc = '/vendor/pdfjs/build/pdf.worker.mjs';
const $ = id => document.getElementById(id);
const side = new URL(location.href).searchParams.get('side') || 'combined';
const nativeHost = new URL(location.href).searchParams.has('native');
history.replaceState(null, '', '/?side=' + side);
let state = null, book = null, pdf = null, pdfTask = null, client = '', desired = null;
let preparing = false, prepared = null, latestRevision = 0, libraryVisible = true;
let renderTasks = [], lastRenderKey = '', currentNodes = [], wheelBudget = 0, wheelTimer = 0;
let flowCount = 0, toastTimer, openSerial = 0, searchSerial = 0;

async function api(route, data) {
 const response = await fetch('/api/' + route, data === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});
 if (!response.ok) throw new Error(await response.text());
 return response.json();
}
function notify(message) { $('toast').textContent = message; $('toast').hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => $('toast').hidden = true, 7000); }
function action(name, number = 0, value = '') { return api('action',{name,number,value,document:state?.document || ''}).catch(e => notify(e.message)); }
function showReader() { libraryVisible=false; $('library').hidden=true; $('reader').hidden=false; $('tools').hidden=false; $('footer').hidden=false; }
function showLibrary() { libraryVisible=true; $('library').hidden=false; $('reader').hidden=true; $('tools').hidden=true; $('footer').hidden=true; $('outline').hidden=true; }
function size() {
 const count=side==='combined'?2:1;
 return {width:Math.max(160,Math.floor(($('reader').clientWidth-36-(count-1)*16)/count)),height:Math.max(200,$('reader').clientHeight-36)};
}
function pair(s) {
 let p=s.cover && s.mode==='spread' && s.page===1 ? [0,1] : [s.page,s.page+1];
 p=p.map(n=>n<1||s.total>0&&n>s.total?0:n);
 if(s.rtl)p.reverse();if(s.swap)p.reverse();return side==='combined'?p:[p[side==='right'?1:0]];
}
function controls(s) {
 document.body.dataset.theme=s.theme;
 $('spreadBtn').classList.toggle('active',s.mode==='spread');$('scrollBtn').classList.toggle('active',s.mode==='scroll');
 for(const [id,on] of [['coverBtn',s.cover],['rtlBtn',s.rtl],['bookmarkBtn',s.bookmarks?.includes(s.page)]])$(id).classList.toggle('active',on);
 $('pageInput').value=s.page;$('pageInput').max=s.total||1;$('pageTotal').textContent='/ '+(s.total||'—');
 $('pairLabel').textContent=pair(s).filter(Boolean).join(' · ');$('previous').disabled=s.page<=1;$('next').disabled=s.total>0&&s.page+(s.mode==='spread'?(s.cover&&s.page===1?1:2):1)>s.total;
 $('fitBtn').textContent=(s.fit==='page'?'Страница':'По ширине')+' '+Math.round(s.zoom*100)+'%';
 $('screenLabel').textContent=side==='combined'?'Две страницы в одном окне':side==='left'?'Левый · экран Windows №1':'Правый · экран Windows №2';
 $('bookmarkBtn').textContent=s.bookmarks?.includes(s.page)?'★':'☆';
 if(book){$('bookName').textContent=book.name;$('formatBadge').textContent=book.format.toUpperCase();for(const id of ['rotateBtn','zoomOut','zoomIn','fitBtn'])$(id).hidden=book.format!=='pdf';$('fontDown').hidden=book.format==='pdf';$('fontUp').hidden=book.format==='pdf';}
 $('marks').replaceChildren();if(s.bookmarks?.length){const h=document.createElement('h3');h.textContent='Закладки';$('marks').append(h);for(const p of s.bookmarks){const btn=document.createElement('button');btn.textContent='Страница '+p;btn.onclick=()=>action('page',p);$('marks').append(btn);}}
}
async function loadBook(id) {
 const serial=++openSerial;
 if(pdfTask){await pdfTask.destroy().catch(()=>{});pdfTask=null;}pdf=null;book=null;flowCount=0;lastRenderKey='';searchSerial++;
 const result=await api('book/'+id);if(serial!==openSerial)return;
 book=result;$('chapters').replaceChildren();
 if(book.format==='pdf'){
  pdfTask=pdfjs.getDocument({url:'/document/'+id,cMapUrl:'/vendor/pdfjs/cmaps/',cMapPacked:true,standardFontDataUrl:'/vendor/pdfjs/standard_fonts/',wasmUrl:'/vendor/pdfjs/wasm/',iccUrl:'/vendor/pdfjs/iccs/',isEvalSupported:false,enableXfa:false});
  pdfTask.onPassword=(update,reason)=>{
   const dialog=$('passwordDialog');$('passwordInput').value='';dialog.querySelector('h2').textContent=reason===2?'Неверный пароль. Попробуйте ещё раз':'PDF защищён паролем';
   dialog.onclose=()=>{if(dialog.returnValue==='ok'){update($('passwordInput').value);$('passwordInput').value='';}else{pdfTask?.destroy();notify('Открытие PDF отменено');}};
   dialog.showModal();$('passwordInput').focus();
  };
  pdf=await pdfTask.promise;if(serial!==openSerial)return;
  if(state?.total!==pdf.numPages)await action('total',pdf.numPages);
  const outline=await pdf.getOutline();
  async function add(items,depth=0){for(const item of items||[]){const button=document.createElement('button');button.textContent=item.title;button.style.paddingLeft=(10+depth*14)+'px';button.onclick=async()=>{try{const dest=typeof item.dest==='string'?await pdf.getDestination(item.dest):item.dest;if(dest){const n=typeof dest[0]==='number'?dest[0]:await pdf.getPageIndex(dest[0]);await action('page',n+1);}}catch(e){notify(e.message)}};$('chapters').append(button);await add(item.items,depth+1)}}
  await add(outline);
 }else{
  for(const entry of book.outline||[]){const btn=document.createElement('button');btn.textContent=entry.title;btn.onclick=()=>goAnchor(entry.target);$('chapters').append(btn);}
 }
 if(!$('chapters').childElementCount)$('chapters').textContent='В книге нет оглавления.';
}
function flowPage(s,w,h,n) {
 const slot=document.createElement('div');slot.className='pageSlot';slot.style.width=w+'px';slot.style.height=h+'px';
 const paper=document.createElement('div');paper.className='paper'+(!n?' blank':'');paper.style.width=w+'px';paper.style.height=h+'px';slot.append(paper);
 if(!n)return {slot,paper,count:0};
 const padding=Math.max(20,Math.round(w*.065));const innerW=w-padding*2,innerH=h-padding*2;
 const clip=document.createElement('div');clip.className='flowClip';Object.assign(clip.style,{width:innerW+'px',height:innerH+'px',margin:padding+'px'});
 const article=document.createElement('article');article.className='flow';article.style.width=innerW+'px';article.style.height=innerH+'px';article.style.columnWidth=innerW+'px';article.style.fontSize=s.font+'px';article.innerHTML=book.html;
 clip.append(article);paper.append(clip);const label=document.createElement('div');label.className='pageNumber';label.textContent=n;paper.append(label);
 return {slot,paper,article,innerW,n,count:0};
}
async function waitImages(root){await Promise.all([...root.querySelectorAll('img')].map(img=>img.complete?Promise.resolve():new Promise(resolve=>{img.onload=resolve;img.onerror=resolve;setTimeout(resolve,5000)})));}
async function makePDF(s,w,h,n) {
 const slot=document.createElement('div');slot.className='pageSlot';Object.assign(slot.style,{width:w+'px',height:h+'px'});
 const paper=document.createElement('div');paper.className='paper pdfPaper'+(!n?' blank':'');slot.append(paper);
 if(!n){paper.style.width=w+'px';paper.style.height=h+'px';return{slot,paper};}
 const page=await pdf.getPage(n);const base=page.getViewport({scale:1,rotation:(page.rotate+s.rotation)%360});
 const scale=(s.fit==='width'?w/base.width:Math.min(w/base.width,h/base.height))*s.zoom;
 const css=page.getViewport({scale,rotation:(page.rotate+s.rotation)%360});
 // Bound each render to 12 megapixels, including HiDPI. No whole-book raster cache.
 const dpr=Math.min(devicePixelRatio||1,2,Math.sqrt(12e6/(css.width*css.height)));
 const canvas=document.createElement('canvas');canvas.width=Math.ceil(css.width*dpr);canvas.height=Math.ceil(css.height*dpr);canvas.style.width=css.width+'px';canvas.style.height=css.height+'px';
 paper.style.width=css.width+'px';paper.style.height=css.height+'px';paper.append(canvas);
 const task=page.render({canvasContext:canvas.getContext('2d',{alpha:false}),viewport:css,transform:dpr!==1?[dpr,0,0,dpr,0,0]:null});renderTasks.push(task);await task.promise;
 const text=document.createElement('div');text.className='textLayer';text.style.setProperty('--total-scale-factor',String(css.scale));text.style.setProperty('--scale-round-x','1px');text.style.setProperty('--scale-round-y','1px');paper.append(text);
 await new pdfjs.TextLayer({textContentSource:await page.getTextContent(),container:text,viewport:css}).render();
 for(const link of await page.getAnnotations({intent:'display'})){
  if(link.subtype!=='Link'||!link.dest||!link.rect)continue;
  const rect=css.convertToViewportRectangle(link.rect),button=document.createElement('button');button.className='pdfLink';button.title='Перейти по ссылке';button.setAttribute('aria-label','Перейти по ссылке');
  Object.assign(button.style,{left:Math.min(rect[0],rect[2])+'px',top:Math.min(rect[1],rect[3])+'px',width:Math.abs(rect[2]-rect[0])+'px',height:Math.abs(rect[3]-rect[1])+'px'});
  button.onclick=async()=>{const dest=typeof link.dest==='string'?await pdf.getDestination(link.dest):link.dest;if(dest){const index=typeof dest[0]==='number'?dest[0]:await pdf.getPageIndex(dest[0]);action('page',index+1);}};paper.append(button);
 }
 return {slot,paper};
}
function applyScroll(s,nodes){for(const n of nodes){const overflow=Math.max(0,n.paper.offsetHeight-n.slot.offsetHeight);n.paper.style.transform=`translateY(${-overflow*s.scroll}px)`;}}
function renderKey(s,w,h){return JSON.stringify([s.document,s.page,s.total,s.mode,s.zoom,s.fit,s.font,s.cover,s.rtl,s.swap,s.rotation,s.anchor,w,h]);}
async function prepare(s) {
 state=s;controls(s);
 if(!s.document){prepared={revision:s.revision,nodes:[],reuse:true};await ack(s);return;}
 if(!book||book.id!==s.document){showReader();$('busy').hidden=false;await loadBook(s.document);}
 if(!book||book.id!==s.document||s.revision!==latestRevision)return;
 const own=size(),w=Math.min(s.width||own.width,own.width),h=Math.min(s.height||own.height,own.height),key=renderKey(s,w,h);
 if(lastRenderKey===key&&currentNodes.length){prepared={revision:s.revision,nodes:currentNodes,reuse:true};await ack(s);return;}
 $('busy').hidden=false;const nodes=[];$('staging').replaceChildren();
 try{
  for(const n of pair(s)){
   const node=book.format==='pdf'?await makePDF(s,w,h,n):flowPage(s,w,h,n);nodes.push(node);$('staging').append(node.slot);
  }
  if(book.format!=='pdf'){
   await waitImages($('staging'));await document.fonts.ready;
   for(const node of nodes){if(!node.article)continue;node.count=Math.max(1,Math.ceil((node.article.scrollWidth-1)/node.innerW));node.article.style.transform=`translateX(${-(node.n-1)*node.innerW}px)`;}
   const count=Math.max(...nodes.map(n=>n.count));flowCount=count;
   if(count>0&&count!==s.total){await action('total',count);return;}
   if(s.anchor){const source=nodes.find(n=>n.article);const target=source?.article.querySelector('#'+CSS.escape(s.anchor));if(target){const desiredPage=1+Math.floor((target.getClientRects()[0].left-source.article.getBoundingClientRect().left+.5)/source.innerW);if(desiredPage!==s.page){await action('page',desiredPage);return;}}}
  }
  if(s.revision!==latestRevision)return;
  prepared={revision:s.revision,nodes,key,reuse:false};await ack(s);
 }finally{renderTasks=[];}
}
async function ack(s){if(!client)return;const own=size();await api('ready',{client,revision:s.revision,width:own.width,height:own.height});}
function commit(s){
 if(!prepared||prepared.revision!==s.revision)return;
 state=s;controls(s);
 if(!prepared.reuse){$('pages').replaceChildren(...prepared.nodes.map(n=>n.slot));currentNodes=prepared.nodes;lastRenderKey=prepared.key;}
 applyScroll(s,currentNodes);$('busy').hidden=true;$('status').textContent=s.mode==='spread'?'Шаг: 2 страницы':'Прокрутка · шаг: 1 страница';
 window.duoDebug={revision:s.revision,pair:pair(s),document:s.document,total:s.total,format:book?.format,name:book?.name,ready:true};
 if(book?.format!=='pdf'){const anchor=readingAnchor();if(anchor)api('action',{name:'locator',number:s.page,value:anchor,document:s.document}).catch(()=>{});}
}
async function pump(){
 if(preparing)return;preparing=true;
 while(desired){const s=desired;desired=null;try{await prepare(s);}catch(e){if(e.name!=='RenderingCancelledException'&&s.revision===latestRevision){$('busy').hidden=true;notify('Не удалось открыть страницу: '+e.message);console.error(e);}}}
 preparing=false;
}
const events=new EventSource('/api/events');
events.addEventListener('hello',e=>{client=JSON.parse(e.data);});
events.onmessage=e=>{const message=JSON.parse(e.data);if(message.kind==='prepare'){latestRevision=message.state.revision;desired=message.state;for(const task of renderTasks)task.cancel();pump();}else if(message.kind==='commit'){commit(message.state);}};
events.onerror=()=>{$('status').textContent='Восстанавливаем соединение…';};

async function loadLibrary(){const entries=await api('library');$('shelf').replaceChildren();$('emptyShelf').hidden=entries.length>0;for(const entry of entries){
 const button=document.createElement('button');button.className='book';
 const cover=document.createElement('div');cover.className='bookCover';const type=document.createElement('span');type.className='coverType';const format=entry.name.toLowerCase().endsWith('.fb2.zip')?'FB2.ZIP':entry.name.split('.').pop().toUpperCase();type.textContent=format;
 const name=document.createElement('span');name.className='coverName';name.textContent=entry.name.replace(/\.(pdf|epub|fb2|fb3|fb2\.zip)$/i,'').replaceAll('_',' ');
 const foot=document.createElement('span');foot.className='coverFoot';foot.textContent='DUO LIBRARY';cover.append(type,name,foot);
 const label=document.createElement('span');label.className='bookName';label.textContent=entry.name;const meta=document.createElement('span');meta.className='bookMeta';meta.textContent=format+' / '+(entry.size/1048576).toFixed(1)+' МБ';button.append(cover,label,meta);
 button.onclick=async()=>{try{showReader();$('busy').hidden=false;await api('open',{id:entry.id});}catch(e){$('busy').hidden=true;notify(e.message);showLibrary();}};$('shelf').append(button);
}}
async function openFile(file){if(!file)return;const data=new FormData();data.append('file',file);$('status').textContent='Открываем…';try{const r=await fetch('/api/upload',{method:'POST',body:data});if(!r.ok)throw new Error(await r.text());showReader();}catch(e){notify(e.message);}}
function goAnchor(id){const source=currentNodes.find(n=>n.article);const target=source?.article.querySelector('#'+CSS.escape(id));if(target){const x=target.getClientRects()[0].left-source.article.getBoundingClientRect().left;action('page',1+Math.floor((x+.5)/source.innerW));$('outline').hidden=true;}}
function readingAnchor(){const source=currentNodes.find(n=>n.article&&n.n===state?.page);if(!source)return '';const clip=source.article.parentElement.getBoundingClientRect();for(const n of source.article.querySelectorAll('[id]')){const r=n.getClientRects()[0];if(r&&r.left>=clip.left-1&&r.left<clip.right&&r.bottom>clip.top)return n.id;}return '';}
async function font(delta){const anchor=readingAnchor();if(anchor)await action('anchor',0,anchor);await action('font',(state?.font||22)+delta);}
async function fullscreen(){if(nativeHost){await action('fullscreen');}else{if(document.fullscreenElement)await document.exitFullscreen();else await document.documentElement.requestFullscreen().catch(()=>action('fullscreen'));}}
$('libraryBtn').onclick=()=>{if(libraryVisible&&book)showReader();else showLibrary();};
for(const id of ['openBtn','addBtn'])$(id).onclick=()=>$('fileInput').click();
$('fileInput').onchange=async e=>{await openFile(e.target.files[0]);e.target.value='';};
$('fullBtn').onclick=fullscreen;
$('spreadBtn').onclick=()=>action('mode',0,'spread');$('scrollBtn').onclick=()=>action('mode',0,'scroll');
$('previous').onclick=()=>action('previous');$('next').onclick=()=>action('next');
$('pageInput').onchange=e=>action('page',Number(e.target.value));
$('zoomIn').onclick=()=>action('zoom',(state?.zoom||1)+.1);$('zoomOut').onclick=()=>action('zoom',(state?.zoom||1)-.1);
$('fitBtn').onclick=()=>action('fit',0,state?.fit==='page'?'width':'page');
$('rotateBtn').onclick=()=>action('rotate');$('fontDown').onclick=()=>font(-2);$('fontUp').onclick=()=>font(2);
$('swapBtn').onclick=()=>action('swap');$('coverBtn').onclick=()=>action('cover');$('rtlBtn').onclick=()=>action('rtl');$('bookmarkBtn').onclick=()=>action('bookmark');
$('themeBtn').onclick=()=>action('theme',0,({paper:'sepia',sepia:'dark',dark:'paper'})[state?.theme||'paper']);
$('outlineBtn').onclick=()=>$('outline').hidden=!$('outline').hidden;$('closeOutline').onclick=()=>$('outline').hidden=true;
async function search(){
 const query=$('searchInput').value.trim().toLocaleLowerCase();if(!query||!book)return;const serial=++searchSerial;
 $('searchBtn').disabled=true;$('status').textContent='Ищем…';
 try{
  if(pdf){for(let step=1;step<=pdf.numPages;step++){if(serial!==searchSerial)return;const n=((state.page-1+step)%pdf.numPages)+1;const p=await pdf.getPage(n);const text=await p.getTextContent();if(text.items.map(i=>i.str||'').join(' ').toLocaleLowerCase().includes(query)){await action('page',n);return;}}}
  else{const source=currentNodes.find(n=>n.article);if(source){const blocks=[...source.article.querySelectorAll('p,h1,h2,h3,li')];for(const block of blocks){if(block.textContent.toLocaleLowerCase().includes(query)){const rect=block.getClientRects()[0];if(rect){const n=1+Math.floor((rect.left-source.article.getBoundingClientRect().left+.5)/source.innerW);await action('page',n);return;}}}}}
  notify('Совпадений не найдено');
 }catch(e){notify(e.message)}finally{$('searchBtn').disabled=false;$('status').textContent='Готово';}
}
$('searchBtn').onclick=search;$('searchInput').addEventListener('keydown',e=>{if(e.key==='Enter')search();});
document.addEventListener('keydown',e=>{
 if(e.ctrlKey&&e.key.toLowerCase()==='f'){e.preventDefault();$('searchInput').focus();return;}
 if(e.ctrlKey&&e.key.toLowerCase()==='o'){e.preventDefault();$('fileInput').click();return;}
 if(e.key==='F11'){e.preventDefault();fullscreen();return;}
 if(['INPUT','TEXTAREA'].includes(e.target.tagName)||$('passwordDialog').open||libraryVisible)return;
 const right=['ArrowRight','PageDown',' '].includes(e.key),left=['ArrowLeft','PageUp'].includes(e.key)||e.shiftKey&&e.key===' ';
 if(right||left){e.preventDefault();action(left?'previous':'next');}
 if(e.key==='Home'){e.preventDefault();action('page',1);}if(e.key==='End'){e.preventDefault();action('page',state?.total||1);}
 if(e.key==='Escape'){$('outline').hidden=true;if(nativeHost)action('fullscreen',0,'exit');}
});
async function wheel(delta){
 if(!state||libraryVisible||Math.abs(delta)<1)return;
 if(state.mode==='scroll'){
  const overflow=Math.max(0,...currentNodes.map(n=>n.paper.offsetHeight-n.slot.offsetHeight));
  if(overflow>2){const next=state.scroll+delta/overflow;if(next>=0&&next<=1){await action('scroll',next);return;}}
 }
 wheelBudget+=delta;clearTimeout(wheelTimer);wheelTimer=setTimeout(()=>wheelBudget=0,180);
 if(Math.abs(wheelBudget)>65){const d=wheelBudget;wheelBudget=0;await action(d>0?'next':'previous');if(d<0&&state?.mode==='scroll')await action('scroll',1);}
}
$('reader').addEventListener('wheel',e=>{e.preventDefault();if(e.ctrlKey){action('zoom',(state?.zoom||1)+(e.deltaY>0?-.1:.1));return;}wheel((e.deltaY||e.deltaX)*(e.deltaMode===1?18:1));},{passive:false});
let pointer=null;$('reader').addEventListener('pointerdown',e=>{if(e.pointerType==='mouse')return;pointer={x:e.clientX,y:e.clientY};});
$('reader').addEventListener('pointerup',e=>{if(!pointer)return;const dx=pointer.x-e.clientX,dy=pointer.y-e.clientY;pointer=null;if(Math.max(Math.abs(dx),Math.abs(dy))>45){if(state?.mode==='spread')action((Math.abs(dx)>Math.abs(dy)?dx:dy)>0?'next':'previous');else wheel(Math.abs(dx)>Math.abs(dy)?dx:dy);}});
let resizing;window.addEventListener('resize',()=>{clearTimeout(resizing);resizing=setTimeout(()=>{lastRenderKey='';if(state){desired=state;pump();}},220);});
document.addEventListener('dragover',e=>{e.preventDefault();document.body.classList.add('drag')});document.addEventListener('dragleave',e=>{if(!e.relatedTarget)document.body.classList.remove('drag')});document.addEventListener('drop',e=>{e.preventDefault();document.body.classList.remove('drag');openFile(e.dataTransfer.files[0]);});
loadLibrary().catch(e=>notify(e.message));
