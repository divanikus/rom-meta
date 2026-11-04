const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const script=readFileSync(require('node:path').join(__dirname,'static/library.js'),'utf8').replace(/librarySyncNavigation\(\);\s*libraryPollOperations\(\);\s*$/,'');
function browser(){
 const elements=new Map(),calls=[],timers=[];
 function element(id){if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',value:'',files:[],listeners:{},classes:new Set(),classList:{add(name){element(id).classes.add(name)},toggle(name,on){on?element(id).classes.add(name):element(id).classes.delete(name)}},setAttribute(){},replaceChildren(){},querySelectorAll:()=>[],addEventListener(k,v){this.listeners[k]=v},contains:()=>false,focus(){}});return elements.get(id);}
 const context=vm.createContext({console,$:element,document:{activeElement:null,querySelectorAll:()=>[],createTextNode:s=>s},
  state:{view:'browse',exportMode:false,platforms:[],platformsLoaded:true,platform:'Console',game:{id:'kept'},games:[{id:'kept'}]},
  catalogUI:{enabled:true},escapeHTML:s=>String(s??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),escapeAttr:s=>String(s??'').replaceAll('"','&quot;').replaceAll('<','&lt;'),formatNumber:n=>String(n??0),
  fetchJSON:async path=>{calls.push(['GET',path]);return []},catalogPost:async(path,body)=>{calls.push(['POST',path,JSON.parse(JSON.stringify(body))]);return {id:body.id,revision:body.revision,state:'importing',filename:'example.dat'}},
  renderPlatforms(){},sourceLabel:s=>s,setView:view=>{context.state.view=view},setExportMode:on=>{context.state.exportMode=on},catalogLeaveInline:()=>true,
  setTimeout:(fn,ms)=>{timers.push(ms);return timers.length},clearTimeout(){},selectPlatform:async()=>{}});
 vm.runInContext(script,context);return {run:c=>vm.runInContext(c,context),element,calls,timers,context};
}
test('reconnection only reads server operations and finds the running job',async()=>{
 const b=browser();b.run('fetchJSON=async()=>[{id:"same-job",state:"importing",filename:"games.dat",progress:{Stage:"writing",Current:4,Total:9}}]');
 await b.run('libraryPollOperations()');
 assert.equal(b.run('libraryUI.selected'),'same-job');
 assert.equal(b.element('operations-count').textContent,'1');
 assert.equal(b.timers.at(-1),1000);
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
 assert.match(b.run('operationDetail(libraryUI.jobs[0])'),/value="4" max="9"/);
});
test('navigation retains selected card and platform; unsaved edit can refuse leaving',()=>{
 const b=browser();b.run('libraryNavigate("operations");libraryNavigate("library")');
 assert.equal(b.run('state.game.id'),'kept');assert.equal(b.run('state.platform'),'Console');
 b.run('catalogLeaveInline=()=>false;libraryNavigate("settings")');
 assert.equal(b.run('state.view'),'browse');
});
test('apply sends exactly the reviewed operation id and revision',async()=>{
 const b=browser();await b.run('libraryImportAction("/api/catalog/import-apply",{id:"chosen",revision:7})');
 assert.deepEqual(b.calls.filter(x=>x[0]==='POST'),[['POST','/api/catalog/import-apply',{id:'chosen',revision:7}]]);
});
test('operation content escapes imported text; running jobs have no apply button',()=>{
 const b=browser(),html=b.run('operationDetail({id:"job",filename:"<script>bad</script>",state:"importing",progress:{Item:"<img src=x>",Stage:"writing"}})');
 assert.match(html,/&lt;script>/);assert.match(html,/&lt;img/);assert.doesNotMatch(html,/<script>|data-import-apply/);
});
test('pending member and platform edits survive progress refresh',()=>{
 const b=browser();b.run('libraryUI.drafts.set("job",{member:"second.dat",platform:"My Console"})');
 const html=b.run('operationDetail({id:"job",filename:"a.zip",state:"needs_selection",members:["first.dat","second.dat"],platform:""})');
 assert.match(html,/value="second.dat" selected/);assert.match(html,/value="My Console"/);
});
test('selected upload survives a background render and read-only mode hides import controls',()=>{
 const b=browser();b.run('libraryUI.file={name:"kept.dat"};libraryRenderOperations()');
 assert.equal(b.element('dat-upload-progress').textContent,'Selected: kept.dat');
 b.run('catalogUI.enabled=false;libraryRenderOperations()');
 assert.equal(b.element('dat-upload-form').classes.has('hidden'),true);
 assert.equal(b.element('library-import').classes.has('hidden'),true);
});
test('a completed job refreshes catalog counts once and is never replayed',async()=>{
 const b=browser();b.run('fetchJSON=async path=>path==="/api/operations"?[{id:"done",state:"succeeded",filename:"example.dat"}]:[{platform:"Console",game_count:2}]');
 await b.run('libraryPollOperations()');await b.run('libraryPollOperations()');
 assert.equal(b.run('state.platforms[0].game_count'),2);
 assert.equal(b.run('libraryUI.refreshed.size'),1);
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
});
test('changed options require a new plan before applying',()=>{
 const b=browser();b.run('libraryUI.drafts.set("job",{member:"a.dat",platform:"Changed"})');
 assert.equal(b.run('libraryPlanDirty({id:"job",member:"a.dat",platform:"Original"})'),true);
 b.run('libraryUpdateApply({id:"job",member:"a.dat",platform:"Original"})');
 assert.match(b.element('dat-apply-hint').textContent,/Prepare the plan again/);
});
test('ready plan offers explicit import and keeps changed options separate',()=>{
 const b=browser(),html=b.run('operationDetail({id:"ready",state:"ready",filename:"example.dat",revision:2,summary:{games:2,releases:2,fingerprints:2,platform:"Console",dataset:"example",added:2,reconciled:0}})');
 assert.match(html,/Import into library/);assert.match(html,/The plan has not changed your cards/);
});
