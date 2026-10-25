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
      addEventListener() {}, querySelectorAll() { return []; },
      classList: {toggle() {}, add() {}, remove() {}}
    });
    return elements.get(id);
  };
  const context = vm.createContext({
    fetch, AbortController, URLSearchParams, Map, console, TextDecoder,
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
