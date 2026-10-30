// No browser/build dependencies: exercise the shipped script with controlled
// fetch timing, so stale responses and a slow summary pass are reproducible.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const vm = require('node:vm');
const html = readFileSync(require('node:path').join(__dirname, 'static/index.html'), 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1].replace(/\s+loadPlatforms\(\);\s*$/, '');

function browser(fetch) {
  const elements = new Map();
  const element = (id) => {
    if (!elements.has(id)) elements.set(id, {
      value: '', checked: false, textContent: '', innerHTML: '', scrollTop: 0,
      listeners: {}, addEventListener(name, handler) { this.listeners[name] = handler; }, querySelectorAll() { return []; },
      attributes: {}, setAttribute(name, value) { this.attributes[name] = value; },
      focus() {}, click() { this.clicked = true; },
      classList: {toggle() {}, add() {}, remove() {}}
    });
    return elements.get(id);
  };
  const context = vm.createContext({
    fetch, AbortController, URLSearchParams, Map, console, TextDecoder, Blob,
    URL: {createObjectURL: () => "blob:test-profile", revokeObjectURL() {}},
    document: {getElementById: element, addEventListener() {}, querySelectorAll() {return [];}}
  });
  vm.runInContext(script, context);
  return {context, element, run: (code) => vm.runInContext(code, context)};
}
const json = (value) => ({ok: true, json: async () => value});
const tick = () => new Promise(setImmediate);
function aborted(signal) {
  return new Promise((resolve,reject) => signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), {name:'AbortError'})), {once:true}));
}

test('first card renders before summaries finish; list rows are bounded and selection preserves them', async () => {
  const calls = [];
  const b = browser(async (path, {signal}) => {
    calls.push(path);
    if (path.includes('names=1')) return json({games:Array.from({length:2000},(_,i)=>({folder:`Game ${i}`}))});
    if (path.startsWith('/api/game?')) return json({game:{folder:'Game 0',title:'First card'}, profile:{}, source_blocks:[],local_media:[]});
    return aborted(signal);
  });
  const loading = b.run('selectPlatform("Arcade")');
  await tick();
  assert.equal(b.element('detail-title').textContent, 'First card');
  assert.equal((b.element('game-list').innerHTML.match(/data-game=/g)||[]).length, 100);
  assert.equal(calls.filter((path)=>path.startsWith('/api/games?platform=')).length, 1);
  const list = b.element('game-list').innerHTML;
  b.element('game-list').scrollTop = 280;
  await b.run('selectGame("Game 1")');
  assert.equal(b.element('game-list').innerHTML, list);
  assert.equal(b.element('game-list').scrollTop, 280);
  b.run('state.platformController.abort()');
  await loading;
});

test('switching platform cancels old work and stale results cannot replace the new list', async () => {
  let oldSignal, oldResult;
  const b = browser(async (path,{signal}) => {
    if (path.includes('platform=Old')) { oldSignal=signal; return new Promise(resolve=>{oldResult=resolve;}); }
    return json({games:[]});
  });
  const oldLoad = b.run('selectPlatform("Old")');
  await b.run('selectPlatform("New")');
  assert.equal(oldSignal.aborted, true);
  oldResult(json({games:[{folder:'Stale'}]}));
  await oldLoad;
  assert.equal(b.run('state.platform'), 'New');
  assert.equal(b.run('state.games.length'), 0);
});

test('search covers undisplayed rows and missing filter excludes unknown/error summaries', () => {
  const b = browser(async()=>json({}));
  b.run('state.games = Array.from({length:2000}, (_,i)=>({folder:"Game "+i})); renderGames()');
  b.element('game-search').value='1999';
  b.run('filterGames()');
  assert.match(b.element('game-list').innerHTML, /Game 1999/);
  b.element('game-search').value=''; b.element('missing-only').checked=true;
  b.run('state.games = [{folder:"Unknown"},{folder:"Broken",summary_error:"read error"},{folder:"Missing",summary_loaded:true,missing_metadata:true}]; renderGames()');
  assert.match(b.element('game-list').innerHTML, /data-game="Missing"/);
  assert.doesNotMatch(b.element('game-list').innerHTML, /data-game="(?:Unknown|Broken)"/);
});


test('stats merges platform totals progressively without duplicate requests', async () => {
  let calls=0;
  const b=browser(async(path)=>{
    if(path==='/api/platforms') return json([{platform:'One'},{platform:'Two'}]);
    calls++;
    assert.match(path,/^\/api\/stats\?platform=/);
    return json({DataRoot:'/catalog',PlatformCount:1,GameCount:3,Platforms:[{Platform:path}],SourceCounts:[{Source:'launchbox',GameCount:3,MatchedGameCount:2}]});
  });
  await b.run('loadStats(true)');
  assert.equal(calls,2);
  assert.equal(b.run('state.stats.GameCount'),6);
  assert.equal(b.run('state.stats.SourceCounts[0].MatchedGameCount'),4);
  assert.match(b.element('stats-pane').innerHTML,/Catalog scan complete/);
});

test('leaving stats cancels the request and stale responses cannot publish totals', async () => {
  let pending,signal;
  const b=browser(async(path,options)=>{
    if(path==='/api/platforms') return json([{platform:'One'}]);
    signal=options.signal;
    return new Promise(resolve=>{pending=resolve;});
  });
  const loading=b.run('loadStats()'); await tick();
  b.run('setView("browse")');
  assert.equal(signal.aborted,true);
  pending(json({GameCount:99})); await loading;
  assert.equal(b.run('state.stats.GameCount || 0'),0);
});


test('video inventory does not fetch or decompress videos before an explicit preview', () => {
 const b=browser(async()=>json({}));
 const markup=b.run('renderMediaSection({local_media:[{type:"video",url:"/media?path=large.mp4",name:"large.mp4",kind:"video"}]})');
 assert.match(markup,/data-lightbox-src="\/media\?path=large.mp4"/);
 assert.match(markup,/Play video/);
 assert.doesNotMatch(markup,/<video|<source|preload=/);
});


test('stats streaming parses UTF-8 split across chunks and rejects incomplete/error streams', async () => {
 function stream(frames) {
  const bytes=new TextEncoder().encode(frames);let index=0;
  return {ok:true,body:{getReader(){return {async read(){if(index===bytes.length)return {done:true};return {done:false,value:bytes.slice(index,index+=1)}} ,async cancel(){}}}}};
 }
 const updates=[];
 const b=browser(async()=>stream(JSON.stringify({progress:{Stage:'reading',Item:'\u30b2\u30fc\u30e0'}})+'\n'+JSON.stringify({result:{GameCount:7}})+'\n'));
 b.context.updates=updates;
 assert.equal((await b.run('fetchStats("Example", new AbortController().signal, p=>updates.push(p))')).GameCount,7);
 assert.equal(updates[0].Item,'\u30b2\u30fc\u30e0');
 b.context.fetch=async()=>stream('{"progress":{}}\n');
 await assert.rejects(b.run('fetchStats("Example", new AbortController().signal, ()=>{})'),/before completion/);
 b.context.fetch=async()=>stream('{"error":"read failed"}\n');
 await assert.rejects(b.run('fetchStats("Example", new AbortController().signal, ()=>{})'),/read failed/);
});


test('stats defaults to the selected platform; a whole catalog scan is explicit', async () => {
 const calls=[];
 const b=browser(async(path)=>{
  if(path==='/api/platforms') return json([{platform:'One'},{platform:'Two'}]);
  calls.push(path); return json({PlatformCount:1,GameCount:3,Platforms:[],SourceCounts:[]});
 });
 b.run('state.platform="Two"'); await b.run('loadStats()');
 assert.equal(calls.length,1); assert.match(calls[0],/platform=Two/);
 assert.match(b.element('stats-pane').innerHTML,/Platform scan complete/);
});


test('empty platform and filtered-out games have distinct guidance', () => {
 const b=browser(async()=>json({}));
 b.run('state.platform=""; state.games=[]; renderGames()');
 assert.match(b.element('game-list').innerHTML,/Select a platform to browse games/);
 b.run('state.platform="Empty"; renderGames()');
 assert.match(b.element('game-list').innerHTML,/No games in this platform/);
 b.run('state.games=[{folder:"Example"}]');
 b.element('game-search').value='absent';
 b.run('renderGames()');
 assert.match(b.element('game-list').innerHTML,/No games match the filter/);
});

test('browser labels source coverage separately from resolved export metadata', () => {
 const b=browser(async()=>json({}));
 b.run('state.game={game:{folder:"Example",releases:[]},profile:{},source_blocks:[],local_media:[],has_metadata:false}; renderDetails()');
 assert.match(b.element('detail-chips').innerHTML,/no source metadata found/);
 assert.match(b.element('detail-body').innerHTML,/Library metadata/);
 b.run('state.game.has_metadata=true; renderDetails()');
 assert.match(b.element('detail-chips').innerHTML,/source metadata matched/);
});


test('export mode projects only visible cards in bounded batches and keeps one policy across navigation', async () => {
  const previewCalls=[];
  const b=browser(async(path,options)=>{
    if(path==='/api/export-preview') {
      const input=JSON.parse(options.body); previewCalls.push(input);
      return json({games:input.games.map(game=>({folder:game.folder,preview:{fields:{title:{value:'Projected '+game.folder},description:{value:'Selected description',source:'local'}},media:{}}}))});
    }
    if(path.includes('names=1')) return json({games:Array.from({length:150},(_,i)=>({folder:'Card '+i}))});
    if(path.startsWith('/api/game?')) return json({game:{folder:'Card 0',title:'Stored title',platform:'New'},profile:{title:'Stored title'},source_blocks:[],local_media:[]});
    return json({games:[]});
  });
  await b.run('state.exportMode=true; state.exportSettings.language="ru"; state.platform="Old"; state.games=Array.from({length:2000},(_,i)=>({folder:"Card "+i})); renderGames()');
  for(let i=0;i<8;i++) await tick();
  assert.equal(previewCalls.reduce((sum,input)=>sum+input.games.length,0),100);
  assert.ok(previewCalls.every(input=>input.games.length<=32 && input.settings.language==='ru'));
  assert.match(b.element('game-list').innerHTML,/Projected Card 0/);
  b.element('game-search').value='Projected Card 0'; b.run('filterGames()');
  assert.match(b.element('game-list').innerHTML,/Projected Card 0/);
  b.element('game-search').value=''; b.run('filterGames()');
  await b.run('selectPlatform("New")');
  for(let i=0;i<10;i++) await tick();
  assert.ok(previewCalls.filter(input=>input.platform==='New').every(input=>input.settings.language==='ru'));
  assert.equal(b.run('state.exportSettings.language'),'ru');
  assert.match(b.element('detail-body').innerHTML,/Selected export description/);
  b.run('state.exportMode=false; resetPreviews(); renderDetails()');
  assert.equal(b.element('detail-title').textContent,'Stored title');
  assert.match(b.element('detail-body').innerHTML,/Library metadata/);
});

test('stale policy and edition requests cannot overwrite current export view', async () => {
  const pending=[];
  const b=browser(async(path,options)=>new Promise(resolve=>pending.push({resolve,signal:options.signal,input:JSON.parse(options.body)})));
  b.run('state.exportMode=true; state.platform="Test"; state.selectedFolder="Card"; state.game={game:{folder:"Card",releases:[{name:"USA"},{name:"Europe"}]}}');
  const old=b.run('loadDetailPreview()');
  b.run('state.edition="release:Europe"');
  const current=b.run('loadDetailPreview()');
  assert.equal(pending[0].signal.aborted,true);
  assert.equal(pending[1].input.games[0].release,'Europe');
  pending[1].resolve(json({games:[{folder:'Card',preview:{release:'Europe',fields:{title:{value:'European title'}},media:{}}}]})); await current;
  pending[0].resolve(json({games:[{folder:'Card',preview:{fields:{title:{value:'Stale title'}},media:{}}}]})); await old;
  assert.equal(b.element('detail-title').textContent,'European title');
  const obsolete=b.run('loadDetailPreview()');
  b.run('resetPreviews(); state.exportMode=false; renderDetails()');
  assert.equal(pending[2].signal.aborted,true);
  pending[2].resolve(json({games:[{preview:{fields:{title:{value:'Wrong policy'}},media:{}}}]})); await obsolete;
  assert.equal(b.run('state.detailPreview'),null);
});

test('saved profile downloads server-validated YAML and keeps paths out of the main view', async () => {
  let input;
  const b=browser(async(path,options)=>{ input=JSON.parse(options.body); return json({target:'launchbox',profile_yaml:'schema_version: 1\nselection:\n  media: {}\n',games:[]}); });
  await b.run('state.exportSettings.target="launchbox"; state.exportSettings.media="none"; saveProfile()');
  assert.equal(input.games.length,0);
  assert.equal(input.settings.media,'none');
  assert.equal(b.element('profile-download').clicked,true);
  assert.equal(b.element('profile-download').href,'blob:test-profile');
  assert.match(b.element('profile-yaml').textContent,/media: \{\}/);
  assert.match(b.element('profile-command').textContent,/--target launchbox/);
  assert.doesNotMatch(b.element('detail-body').innerHTML,/--rom-root/);
});


test('game-level export rows never display an implicit default edition', () => {
  const b=browser(async()=>json({}));
  b.run('state.exportMode=true; state.games=[{folder:"Card",default_release:"Implicit USA",previewRevision:0,preview:{fields:{title:{value:"Card"}},media:{}}}]; renderGames()');
  assert.match(b.element('game-list').innerHTML,/Game-level selection/);
  assert.doesNotMatch(b.element('game-list').innerHTML,/Implicit USA/);
});


test('explicit modes keep provider descriptions outside export preview', () => {
  const b=browser(async()=>json({}));
  b.run(`state.game={game:{folder:"Card"},profile:{description:"Stored prose"},source_blocks:[{source:"launchbox",match_count:1,profile:{descriptions:[{language:"en",text:"Unselected provider prose"}]}}],local_media:[]}; state.detailPreview={fields:{title:{value:"Card",source:"catalog"},description:{value:"Chosen prose",source:"screenscraper",language:"ru"}},media:{}}; state.exportMode=true; renderDetails()`);
  assert.match(b.element('detail-body').innerHTML,/Chosen prose/);
  assert.match(b.element('detail-body').innerHTML,/ScreenScraper/);
  assert.doesNotMatch(b.element('detail-body').innerHTML,/Unselected provider prose|Stored source evidence|data-source-index/);
  b.run('setExportMode(false)');
  assert.match(b.element('detail-body').innerHTML,/Unselected provider prose/);
  assert.equal(b.element('library-mode').attributes['aria-pressed'],'true');
  assert.equal(b.element('export-mode').attributes['aria-pressed'],'false');
});

test('visual source chooser loads catalog defaults, reorders, disables all and resets without text input', async () => {
  const b=browser(async(path)=>{ assert.equal(path,'/api/export-options');return json({metadata_sources:['launchbox','screenscraper'],artwork_sources:['emumovies','launchbox'],video_sources:['emumovies']}); });
  await b.run('loadExportSources()');
  assert.match(b.element('export-source-controls').innerHTML,/ScreenScraper/);
  assert.doesNotMatch(html,/comma-separated override|id="export-metadata-sources"/);
  b.element('export-source-controls').listeners.click({target:{closest:()=>({dataset:{sourceCategory:'metadata_sources',sourceId:'screenscraper',sourceAction:'up'}})}});
  assert.equal(b.run('state.exportSettings.metadata_sources.join(",")'),'screenscraper,launchbox');
  b.run('changeSourcePreference("metadata_sources","screenscraper","disable"); changeSourcePreference("metadata_sources","launchbox","disable")');
  assert.equal(b.run('JSON.stringify(state.exportSettings.metadata_sources)'),'[]');
  assert.match(b.element('export-source-controls').innerHTML,/Automatic sources are off/);
  b.run('changeSourcePreference("metadata_sources",null,"reset")');
  assert.equal(b.run('Object.hasOwn(state.exportSettings,"metadata_sources")'),false);
  assert.equal(b.run('sourcePreference("metadata_sources").join(",")'),'launchbox,screenscraper');
});

test('release inspection says what changed and explains equal shared output', async () => {
  let name='Europe';
  const b=browser(async(path,options)=>{
    const input=JSON.parse(options.body).games[0];assert.equal(input.release,name);
    return json({games:[{preview:{release:name,fields:{title:{value:'European title',source:'release'},description:{value:'Shared prose',source:'launchbox'}},media:{cover:{source_path:'cover.png'}}}}]});
  });
  b.run(`state.exportMode=true; state.selectedFolder="Card"; state.game={game:{folder:"Card",releases:[{name:"USA"},{name:"Europe"}]}}; state.detailPreview={release:"USA",fields:{title:{value:"American title",source:"release"},description:{value:"Shared prose",source:"launchbox"}},media:{cover:{source_path:"cover.png"}}}`);
  await b.run('inspectEdition("release:Europe")');
  assert.match(b.element('detail-body').innerHTML,/Updated for this release: title/);
  name='USA'; // Different edition, identical selected output.
  await b.run('inspectEdition("release:USA")');
  assert.match(b.element('detail-body').innerHTML,/unchanged under this profile/);
  assert.equal(b.element('export-edition').value,'release:USA');
  assert.match(b.element('detail-body').innerHTML,/Shared game data can be reused/);
});
