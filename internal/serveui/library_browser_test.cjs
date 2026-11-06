const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const script=readFileSync(require('node:path').join(__dirname,'static/library.js'),'utf8').replace(/librarySyncNavigation\(\);\s*libraryPollOperations\(\);\s*$/,'');
function browser(){
 const elements=new Map(),calls=[],timers=[];
 function element(id){if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',value:'',files:[],listeners:{},classes:new Set(),classList:{remove(name){element(id).classes.delete(name)},add(name){element(id).classes.add(name)},toggle(name,on){on?element(id).classes.add(name):element(id).classes.delete(name)}},setAttribute(){},replaceChildren(){},querySelectorAll:()=>[],addEventListener(k,v){this.listeners[k]=v},contains:()=>false,focus(){}});return elements.get(id);}
 const context=vm.createContext({console,URLSearchParams,$:element,window:{addEventListener(){},confirm:()=>true},document:{documentElement:{dataset:{}},addEventListener(){},activeElement:null,querySelectorAll:()=>[],createTextNode:s=>s},
  state:{view:'browse',exportMode:false,platforms:[],platformsLoaded:true,platform:'Console',game:{id:'kept'},games:[{id:'kept'}]},
  catalogUI:{enabled:true},escapeHTML:s=>String(s??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),escapeAttr:s=>String(s??'').replaceAll('"','&quot;').replaceAll('<','&lt;'),formatNumber:n=>String(n??0),
  fetchJSON:async path=>{calls.push(['GET',path]);return []},catalogPost:async(path,body)=>{calls.push(['POST',path,JSON.parse(JSON.stringify(body))]);return {id:body.id,revision:body.revision,state:'importing',filename:'example.dat'}},
  platformMenuClose(){},renderPlatforms(){},sourceLabel:s=>s,setView:view=>{context.state.view=view},setExportMode:on=>{context.state.exportMode=on},catalogLeaveInline:()=>true,
  setTimeout:(fn,ms)=>{timers.push(ms);return timers.length},clearTimeout(){},selectPlatform:async()=>{}});
 vm.runInContext(readFileSync(require('node:path').join(__dirname,'static/export.js'),'utf8'),context);vm.runInContext(script,context);vm.runInContext(readFileSync(require('node:path').join(__dirname,'static/enrichment.js'),'utf8'),context);return {run:c=>vm.runInContext(c,context),element,calls,timers,context};
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

test('operations has one Add job menu on both empty and selected-job screens',()=>{
 const b=browser();
 for(const selected of [false,true]){
  if(selected)b.run('libraryUI.jobs=[{id:"job",state:"ready",filename:"a.dat"}];libraryUI.selected="job"');
  b.run('libraryRenderOperations()');
  const html=b.element('operations-pane').innerHTML;
  assert.equal((html.match(/id="operations-add-job"/g)||[]).length,1);
  assert.doesNotMatch(html,/data-import-new|data-enrichment-new/);
  assert.match(html,/data-new-job="import"/);assert.match(html,/data-new-job="fetch" disabled/);assert.match(html,/data-new-job="export" disabled/);
 }
 b.run('state.platforms=[{platform:"Console"}];libraryRenderOperations()');
 assert.doesNotMatch(b.element('operations-pane').innerHTML,/data-new-job="(?:fetch|export)" disabled/);
 b.run('catalogUI.enabled=false;libraryRenderOperations()');
 assert.doesNotMatch(b.element('operations-pane').innerHTML,/data-new-job="(?:import|fetch|export)"/);
 assert.match(b.element('operations-pane').innerHTML,/data-new-job="preview"/);
});
function configureSettings(b){
 b.run('state.view="settings";librarySettingsUI.sections=[{id:"priorities",title:"Source priorities",help:"Saved defaults",file:"config/sources.yaml",revision:"before",fields:[{id:"metadata",label:"Metadata",kind:"list",value:["launchbox","igdb"],choices:["launchbox","igdb"]}]}]');
}
test('settings drafts survive navigation and list reordering changes only the draft',()=>{
 const b=browser();configureSettings(b);
 b.run('settingsListChange("metadata","igdb","up");libraryNavigate("operations");libraryNavigate("settings")');
 assert.equal(b.run('settingsValue(settingsSection(),settingsSection().fields[0]).join(",")'),'igdb,launchbox');
 assert.equal(b.run('settingsSection().fields[0].value.join(",")'),'launchbox,igdb');
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
});
test('settings save sends explicit empty list with the loaded revision',async()=>{
 const b=browser();configureSettings(b);b.run('settingsChange("metadata",[]);fetchJSON=async path=>path==="/api/catalog/settings"?librarySettingsUI.sections:{}');
 await b.run('librarySaveSettings()');
 assert.deepEqual(b.calls.filter(x=>x[0]==='POST'),[['POST','/api/catalog/settings',{section:'priorities',revision:'before',values:{metadata:[]}}]]);
 assert.equal(b.run('librarySettingsUI.drafts.size'),0);
});
test('a stale settings save retains edits and reports the conflict',async()=>{
 const b=browser();configureSettings(b);b.run('settingsChange("metadata",[]);catalogPost=async()=>{throw new Error("Settings changed; reload")}');
 await b.run('librarySaveSettings()');
 assert.equal(b.run('librarySettingsUI.drafts.size'),1);
 assert.match(b.run('librarySettingsUI.error'),/Settings changed/);
});
test('refresh failure after a committed settings save is shown as saved',async()=>{
 const b=browser();configureSettings(b);b.run('settingsChange("metadata",[]);catalogPost=async()=>({saved:true,revision:"after"});fetchJSON=async()=>{throw new Error("Offline")}');
 await b.run('librarySaveSettings()');
 assert.equal(b.run('librarySettingsUI.drafts.size'),0);assert.equal(b.run('librarySettingsUI.error'),'');
 assert.match(b.run('librarySettingsUI.notice'),/Saved to library.*Offline/);
 assert.equal(b.run('settingsSection().revision'),'after');
});

test('saving one section keeps other same-file drafts saveable but preserves older conflicts',async()=>{
 const b=browser();configureSettings(b);
 b.run('librarySettingsUI.sections.push({id:"fetch",title:"Enrichment",file:"config/sources.yaml",revision:"before",fields:[]},{id:"source:igdb",title:"IGDB",file:"config/sources.yaml",revision:"before",fields:[]});librarySettingsUI.drafts.set("fetch",{revision:"before",values:{concurrency:3}});librarySettingsUI.drafts.set("source:igdb",{revision:"older",values:{concurrency:2}});settingsChange("metadata",[]);catalogPost=async()=>({saved:true,revision:"after"});fetchJSON=async path=>path==="/api/catalog/settings"?librarySettingsUI.sections:{}');
 await b.run('librarySaveSettings()');
 assert.equal(b.run('librarySettingsUI.drafts.get("fetch").revision'),'after');
 assert.equal(b.run('librarySettingsUI.drafts.get("fetch").values.concurrency'),3);
 assert.equal(b.run('librarySettingsUI.drafts.get("source:igdb").revision'),'older');
});

test('enrichment reconnection discovers running work without replaying a start',async()=>{
 const b=browser();b.run('state.view="operations";fetchJSON=async()=>[{id:"running",kind:"enrichment",platform:"Console",state:"enriching",enrichment:{current:0,progress:{Stage:"matching",Current:3,Total:9},jobs:[{source:"launchbox",type:"metadata",state:"running"}]}}]');
 await b.run('libraryPollOperations()');
 assert.equal(b.run('libraryUI.selected'),'running');assert.equal(b.timers.at(-1),1000);
 assert.equal(b.calls.filter(c=>c[0]==='POST').length,0);
 assert.match(b.element('operations-pane').innerHTML,/3 \/ 9 games/);
 assert.doesNotMatch(b.element('operations-pane').innerHTML,/data-enrichment-start=|data-import-apply=/);
});
test('enrichment draft keeps choices across polling and ignores unavailable source selections',async()=>{
 const b=browser();b.run('state.view="operations";enrichmentUI.draft={platform:"Console",platforms:["Console"],concurrency:0,revision:"r1",choices:[{source:"launchbox",type:"metadata",available:true,default:true},{source:"igdb",type:"metadata",available:false,reason:"Credentials missing"}],selected:new Set(["launchbox:metadata","igdb:metadata"])};libraryUI.selected=""');
 await b.run('libraryPollOperations()');
 assert.equal(b.run('enrichmentSelected().length'),1);
 assert.match(b.element('operations-pane').innerHTML,/Credentials missing/);
 assert.match(b.element('operations-pane').innerHTML,/Review tasks/);
 assert.equal(b.run('enrichmentUI.draft.selected.has("launchbox:metadata")'),true);
 b.run('libraryOpenImport()');assert.equal(b.run('enrichmentUI.draft'),null);
});
test('enrichment prepares only reviewed source/type pairs for the exact platform',async()=>{
 const b=browser();const op={id:'prepared',kind:'enrichment',platform:'Console',revision:1,state:'ready',enrichment:{jobs:[{source:'launchbox',type:'metadata',state:'pending'}]}};
 b.context.catalogPost=async(path,body)=>{b.calls.push(['POST',path,JSON.parse(JSON.stringify(body))]);return op};
 b.context.fetchJSON=async()=>[op];
 b.run('state.view="operations";enrichmentUI.draft={platform:"Console",platforms:["Console"],concurrency:4,revision:"original",choices:[{source:"launchbox",type:"metadata",available:true},{source:"launchbox",type:"artwork",available:true}],selected:new Set(["launchbox:metadata"])}');
 await b.run('enrichmentPrepare()');
 assert.deepEqual(b.calls.filter(c=>c[0]==='POST'),[['POST','/api/catalog/enrichment-plan',{platform:'Console',platforms:['Console'],concurrency:4,revision:'original',jobs:[{source:'launchbox',type:'metadata'}]}]]);
 assert.equal(b.run('libraryUI.selected'),'prepared');assert.equal(b.run('enrichmentUI.draft'),null);
 assert.match(b.element('operations-pane').innerHTML,/>Start fetching<\/button>/);
});
test('fetch action distinguishes runnable plans from active and queued operations',()=>{
 const b=browser();b.run('plan={id:"new",kind:"enrichment",platform:"Console",state:"ready",enrichment:{jobs:[]}};libraryUI.jobs=[plan]');
 assert.match(b.run('enrichmentDetail(plan)'),/>Start fetching<\/button>/);
 for(const state of ['planning','importing','enriching','exporting','queued']){
  b.context.otherState=state;b.run('libraryUI.jobs=[plan,{id:"other",state:otherState}]');
  assert.match(b.run('enrichmentDetail(plan)'),/>Enqueue<\/button>/,state);
 }
 for(const state of ['ready','needs_review','succeeded','incomplete','failed']){
  b.context.otherState=state;b.run('libraryUI.jobs=[plan,{id:"other",state:otherState}]');
  assert.match(b.run('enrichmentDetail(plan)'),/>Start fetching<\/button>/,state);
 }
});

test('ready fetch action returns to Start after the active operation finishes',async()=>{
 const b=browser();
 const plan={id:'new',kind:'enrichment',platform:'Console',state:'ready',enrichment:{jobs:[]}};
 const other={id:'other',filename:'example.dat',state:'importing'};
 b.context.fetchJSON=async path=>path==='/api/operations'?JSON.parse(JSON.stringify([plan,other])):[];
 b.run('state.view="operations";libraryUI.selected="new"');
 await b.run('libraryPollOperations()');
 assert.match(b.element('operations-pane').innerHTML,/>Enqueue<\/button>/);
 other.state='succeeded';await b.run('libraryPollOperations()');
 assert.match(b.element('operations-pane').innerHTML,/>Start fetching<\/button>/);
 assert.equal(b.calls.filter(c=>c[0]==='POST').length,0);
});

test('enrichment partial completion refreshes data once and preserves an active editor',async()=>{
 const b=browser();let reloads=0;
 b.context.selectPlatform=async()=>{reloads++};b.context.catalogEditorActive=()=>true;
 const op={id:'partial',kind:'enrichment',platform:'Console',state:'incomplete',enrichment:{jobs:[{source:'launchbox',type:'artwork',state:'incomplete',counts:{downloaded:2,failed:1},error:'<img src=x onerror=alert(1)>'}]}};
 b.context.fetchJSON=async path=>path==='/api/operations'?[op]:[{platform:'Console',game_count:9}];
 await b.run('libraryPollOperations()');await b.run('libraryPollOperations()');
 assert.equal(reloads,0);assert.equal(b.run('libraryUI.refreshed.size'),1);
 b.context.partial=op;
 const html=b.run('operationDetail(partial)');assert.match(html,/Downloaded<\/dt><dd>2/);assert.match(html,/Prepare another pass/);assert.match(html,/&lt;img/);assert.doesNotMatch(html,/<img/);
});
test('late enrichment choices cannot replace a newer platform or a new import',async()=>{
 const b=browser(),resolve={};
 b.context.fetchJSON=path=>path==='/api/operations'?Promise.resolve([]):new Promise(r=>{resolve[new URL('http://test'+path).searchParams.get('platform')]=r});
 const first=b.run('enrichmentOpen("First")'),second=b.run('enrichmentOpen("Second")');
 resolve.Second({platform:'Second',revision:'2',choices:[]});await second;
 resolve.First({platform:'First',revision:'1',choices:[]});await first;
 assert.equal(b.run('enrichmentUI.draft.platform'),'Second');
 const third=b.run('enrichmentOpen("Third")');b.run('libraryOpenImport()');
 resolve.Third({platform:'Third',revision:'3',choices:[]});await third;
 assert.equal(b.run('enrichmentUI.draft'),null);
});

test('export navigation opens setup and keeps a draft through other views',async()=>{
 const b=browser();b.run('nativeExportUI.loaded=true;nativeExportUI.draft={...exportDraft(),name:"ES-DE couch",path:"/frontends/esde",rom_root:"/roms"};libraryNavigate("export")');
 assert.equal(b.run('state.view'),'export');assert.match(b.element('native-export-pane').innerHTML,/ES-DE couch/);
 b.run('libraryNavigate("library");libraryNavigate("export")');
 assert.equal(b.run('nativeExportUI.draft.path'),'/frontends/esde');
 assert.equal(b.calls.filter(x=>x[0]==='POST'&&x[1]!=='/api/export/profile').length,0);
});
test('export reconnect reads progress without replaying planning or apply',async()=>{
 const b=browser();
 b.run('state.view="operations";fetchJSON=async()=>[{id:"exp1",kind:"export",state:"exporting",export:{destination:{name:"Couch",target:"es-de",path:"/output",rom_root:"/roms",platform:"n64"},progress:{Stage:"write",Item:"gamelist.xml",Current:1,Total:3},written:1}}]');
 await b.run('libraryPollOperations()');
 assert.equal(b.run('libraryUI.selected'),'exp1');assert.equal(b.timers.at(-1),1000);
 assert.match(b.element('operations-pane').innerHTML,/1 \/ 3/);assert.doesNotMatch(b.element('operations-pane').innerHTML,/data-native-export-apply/);
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
});
test('export prepare freezes chosen paths and uploaded profile without applying',async()=>{
 const b=browser();b.run('state.view="export";nativeExportUI.loaded=true;nativeExportUI.draft={...exportDraft(),profile_yaml:"schema_version: 1\\n",profile_name:"curated.yaml"}');
 for(const [id,value]of Object.entries({'export-destination-name':'Couch','export-run-target':'es-de','export-destination-path':'/frontend','export-rom-root':'/roms/n64','export-run-platform':'n64','export-media-root':'','export-profile-mode':'yaml','export-rom-mode':'single'}))b.element(id).value=value;
 const op={id:'plan1',kind:'export',state:'planning',revision:1,export:{destination:{name:'Couch'},progress:{}}};
 b.context.catalogPost=async(path,body)=>{b.calls.push(['POST',path,JSON.parse(JSON.stringify(body))]);return op};
 b.context.fetchJSON=async()=>[op];
 await b.run('exportPrepare()');
 const posts=b.calls.filter(x=>x[0]==='POST');assert.equal(posts.length,1);assert.equal(posts[0][1],'/api/export/prepare');
 assert.equal(posts[0][2].destination.path,'/frontend');assert.equal(posts[0][2].destination.rom_root,'/roms/n64');assert.equal(posts[0][2].destination.profile_yaml,'schema_version: 1\n');
 assert.equal(b.run('state.view'),'operations');assert.equal(b.run('libraryUI.selected'),'plan1');
});
test('export conflicts block apply and partial results retain readable counts',()=>{
 const b=browser();b.context.job={id:'x',kind:'export',state:'needs_review',export:{destination:{name:'<img>',path:'/out',rom_root:'/rom'},written:0,totals:{matched:1,unmatched:2,ambiguous:0,unsupported:0,files:3,games:0,changes:[],conflicts:['<script>bad</script>'],warnings:[],skipped:[]}}};
 const html=b.run('nativeExportDetail(job)');assert.doesNotMatch(html,/data-native-export-apply|<script>|<img>/);assert.match(html,/&lt;script/);assert.match(html,/Download full plan/);
 b.run('job.state="incomplete";job.export.written=1');const partial=b.run('nativeExportDetail(job)');assert.match(partial,/Some files were written/);assert.match(partial,/Prepare another plan/);
});
test('late folder replies cannot reopen a closed picker or replace a newer path',async()=>{
 const b=browser(),resolve={};
 b.context.fetchJSON=path=>new Promise(r=>{resolve[new URL('http://test'+path).searchParams.get('path')]=r});
 const first=b.run('exportBrowse("rom_root","/first")'),second=b.run('exportBrowse("rom_root","/second")');
 resolve['/second']({path:'/second',parent:'/',folders:[]});await second;
 resolve['/first']({path:'/first',parent:'/',folders:[]});await first;
 assert.equal(b.run('nativeExportUI.folder.path'),'/second');
 const third=b.run('exportBrowse("path","/third")');b.run('exportCloseFolder()');resolve['/third']({path:'/third',folders:[]});await third;
 assert.equal(b.run('nativeExportUI.folder'),null);
});


test('unchanged export polling preserves expanded details and focus',async()=>{
 const b=browser();const op={id:'steady',kind:'export',state:'ready',export:{destination:{name:'Couch',path:'/out',rom_root:'/roms'},totals:{files:1,games:0,changes:[]},progress:{}}};
 b.context.fetchJSON=async()=>[op];b.run('state.view="operations"');
 await b.run('libraryPollOperations()');
 b.element('operations-pane').innerHTML='user is reviewing expanded details';
 await b.run('libraryPollOperations()');
 assert.equal(b.element('operations-pane').innerHTML,'user is reviewing expanded details');
});

test('opening a completed DAT import dismisses only its receipt',async()=>{
 const b=browser();b.context.catalogPost=async(path,body)=>{b.calls.push([path,body.id]);return {dismissed:true}};
 await b.run('libraryOpenCompleted({id:"dat",kind:"dat-import",state:"succeeded",platform:"Console",revision:2})');
 assert.equal(b.run('state.view'),'browse');assert.deepEqual(b.calls.filter(x=>x[0]==="/api/operations/dismiss"),[["/api/operations/dismiss","dat"]]);
 await b.run('libraryOpenCompleted({id:"enrich",kind:"enrichment",state:"succeeded",platform:"Console",revision:2})');
 assert.equal(b.calls.filter(x=>x[0]==="/api/operations/dismiss").length,1);
});
test('queued enrichment reconnect exposes automatic start and remove, never Start again',()=>{
 const b=browser();b.run('libraryUI.jobs=[{id:"q",kind:"enrichment",platform:"Console",state:"queued",queued_at:"1",enrichment:{platforms:["Console","Other"],jobs:[{source:"launchbox",type:"artwork",state:"pending",platform:"Console",workers:4}]}}]');
 const html=b.run('enrichmentDetail(libraryUI.jobs[0])');
 assert.equal(b.run('operationBusy(libraryUI.jobs[0])'),true);assert.match(html,/Starts automatically/);assert.match(html,/Remove from queue/);assert.doesNotMatch(html,/data-enrichment-start/);
});
test('enrichment platform changes fetch all choices and preserve explicit source choices',async()=>{
 const b=browser();b.context.fetchJSON=async path=>path==="/api/operations"?[]:{revision:"settings",choices:[{source:"launchbox",type:"metadata",available:true,default:true},{source:"igdb",type:"metadata",available:false,reason:"Credentials missing"}]};
 b.run('state.platforms=[{platform:"Console"},{platform:"Other"}]');
 await b.run('enrichmentOpen(["Console","Other"],[{source:"launchbox",type:"metadata"}],4)');
 assert.equal(b.run('enrichmentUI.draft.platforms.length'),2);assert.equal(b.run('enrichmentUI.draft.choices[0].platform_count'),2);assert.equal(b.run('enrichmentUI.draft.concurrency'),4);
 assert.match(b.run('enrichmentWizard()'),/Platforms \(2\)/);
});
test('export review shows evidence, pages problem inputs and keeps exclusions across renders',()=>{
 const b=browser();b.context.op={id:"review",revision:1,kind:"export",state:"ready",export:{excluded:[],totals:{issues:Array.from({length:81},(_,i)=>({id:"id"+i,rom:"Problem "+i+".bin",platform:"Console",status:"ambiguous",reason:"Multiple releases",candidates:["One","Two"]}))}}};
 const html=b.run('exportIssues(op)');assert.match(html,/Candidates: One; Two/);assert.match(html,/1 \/ 3/);assert.doesNotMatch(html,/Problem 40/);
 b.run('exportIssueDraft(op).excluded.add("id0")');assert.equal(b.run('exportReviewDirty(op)'),true);assert.match(b.run('exportIssues(op)'),/data-export-exclude="id0" checked/);
 b.run('op.revision=2;op.export.excluded=["id0"]');assert.equal(b.run('exportReviewDirty(op)'),false);
});
test('inline defaults load library policy without using compact browser settings',async()=>{
 const b=browser();b.context.catalogPost=async()=>({profile_yaml:'selection: saved-priorities',profile:{selection:{description:{sources:['manual.notes']}}}});b.run('nativeExportUI.draft=exportDraft()');
 await b.run('exportEnsurePolicy()');assert.equal(b.run('nativeExportUI.draft.policy.selection.description.sources[0]'),'manual.notes');
});
test('theme toggle applies both themes to the document',()=>{
 const b=browser();b.run('libraryTheme("light")');assert.equal(b.context.document.documentElement.dataset.theme,'light');assert.equal(b.element('theme-toggle').title,'Switch to dark theme');
 b.run('libraryTheme("dark")');assert.equal(b.context.document.documentElement.dataset.theme,'dark');
});


test('profile editing cannot leak UI-only fields into export requests',async()=>{
 const b=browser();b.run('nativeExportUI.draft={...exportDraft(),name:"Test",ruleKey:"description",prioritiesOpen:true,profile_mode:"defaults"};exportCapture=()=>{}');
 const input=await b.run('exportDestinationInput()');assert.equal(input.name,'Test');assert.equal('ruleKey' in input,false);assert.equal('prioritiesOpen' in input,false);assert.equal('profile_mode' in input,false);
});

test('platform selection feeds both inline actions and survives browsing',async()=>{
 const b=browser();b.run('state.platforms=[{platform:"Console"},{platform:"Other"}];state.platformSelection=new Set(["Console","Other","Gone"]);librarySyncPlatformSelection();');
 assert.equal(b.run('librarySelectedPlatforms().join(",")'),'Console,Other');
 assert.equal(b.element('platform-selected-count').textContent,'2 selected');
 b.run('enrichmentOpen=scope=>{state.fetchScope=scope};catalogBuildPlan=(manual,resume,scope)=>{state.foldScope=scope};');
 await b.element('platform-action-fetch').listeners.click();
 await b.element('platform-action-fold').listeners.click();
 assert.equal(b.run('state.fetchScope.join(",")'),'Console,Other');assert.equal(b.run('state.foldScope.join(",")'),'Console,Other');
 b.run('state.platform="Other";librarySyncPlatformSelection()');assert.equal(b.run('librarySelectedPlatforms().length'),2);
});
test('platform actions fall back to the browsed platform and respect read-only mode',()=>{
 const b=browser();b.run('librarySyncPlatformSelection()');
 assert.equal(b.run('libraryPlatformScope().join(",")'),'Console');
 assert.equal(b.element('platform-selected-count').textContent,'Current platform');
 assert.equal(b.element('platform-action-fold').disabled,false);
 b.run('catalogUI.enabled=false;librarySyncPlatformSelection()');assert.equal(b.element('platform-action-fold').disabled,true);assert.equal(b.element('platform-action-stats').disabled,false);
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
});

test('Add job opens the chosen setup without starting a mutation',()=>{
 const b=browser();b.run('libraryOpenImport=()=>{state.setup="import"};enrichmentOpen=platforms=>{state.setup="fetch";state.chosen=platforms};libraryNavigate=view=>{state.setup=view}');
 b.run('libraryStartJob("import")');assert.equal(b.run('state.setup'),'import');
 b.run('libraryStartJob("fetch")');assert.equal(b.run('state.setup'),'import');
 b.run('state.platforms=[{platform:"Console"}];libraryStartJob("fetch")');assert.equal(b.run('state.setup'),'fetch');assert.equal(b.run('state.chosen.length'),0);
 b.run('libraryStartJob("export")');assert.equal(b.run('state.setup'),'export');
 b.run('catalogUI.enabled=false;libraryStartJob("import")');assert.equal(b.run('state.setup'),'export');
 assert.equal(b.calls.filter(c=>c[0]==='POST').length,0);
});
test('operation refresh keeps an open Add job menu and keyboard focus',()=>{
 const b=browser(),menu=b.element('operations-add-job'),item=b.element('operation-job-fetch');
 menu.open=true;menu.contains=e=>e===item;item.id='operation-job-fetch';
 b.context.document.activeElement=item;let focused=false;item.focus=()=>{focused=true};
 b.run('libraryRenderOperations()');
 assert.match(b.element('operations-pane').innerHTML,/id="operations-add-job" open/);assert.equal(focused,true);
});

test('export setup and profile preview belong to Operations in the header',()=>{
 const b=browser();b.run('state.view="export";librarySyncNavigation()');
 assert.equal(b.element('operations-tab').classes.has('active'),true);
 assert.equal(b.element('browse-tab').classes.has('active'),false);
 b.run('state.view="browse";state.exportMode=true;librarySyncNavigation()');
 assert.equal(b.element('operations-tab').classes.has('active'),true);
 b.run('state.exportMode=false;librarySyncNavigation()');
 assert.equal(b.element('browse-tab').classes.has('active'),true);
 b.run('catalogUI.enabled=false;state.platforms=[{platform:"Console"}];exportPreviewProfile=()=>{state.preview=true};libraryStartJob("preview")');
 assert.equal(b.run('state.preview'),true);assert.equal(b.calls.filter(c=>c[0]==='POST').length,0);
});


test('named profile save stores policy separately, update and copy retain explicit identity',async()=>{
 const b=browser();profileSetup(b);b.element('export-saved-profile-name').value='My images';
 const requests=[];let version=0;
 b.context.catalogPost=async(path,body)=>{
  requests.push([path,JSON.parse(JSON.stringify(body))]);
  if(path==='/api/export/profile')return{profile:body.profile,profile_yaml:'validated',target:'es-de'};
  assert.equal(path,'/api/export/profiles');assert.equal(body.revision,version?'r'+version:'');
  version++;return{revision:'r'+version,profiles:[{...body.profile,id:body.profile.id||'p'+version}]};
 };
 await b.run('exportSaveProfile()');assert.equal(b.run('nativeExportUI.draft.profile_id'),'p1');assert.equal(b.run('nativeExportUI.draft.path'),'/output');
 assert.equal(requests[1][1].profile.name,'My images');assert.equal(requests[1][1].profile.path,undefined);
 b.run('exportPolicyChanged()');assert.equal(b.run('nativeExportUI.draft.profile_id'),'');assert.equal(b.run('nativeExportUI.draft.editProfileID'),'p1');
 await b.run('exportSaveProfile()');assert.equal(requests.at(-1)[1].profile.id,'p1');
 b.element('export-saved-profile-name').value='Another variant';await b.run('exportSaveProfile(true)');
 assert.equal(requests.at(-1)[1].profile.id,'');assert.equal(b.run('nativeExportUI.draft.profile_id'),'p3');
 assert.equal(requests.some(([path])=>path==='/api/export/destinations'||path==='/api/export/prepare'),false);
});
test('saved profile selection restores policy and missing references never fall back silently',async()=>{
 const b=browser();profileSetup(b);const policy=b.run('nativeExportUI.draft.policy');
 b.context.catalogPost=async()=>({profile:policy,profile_yaml:'loaded'});
 b.run('nativeExportUI.profiles=[{id:"p",name:"Saved",target:"es-de",profile_yaml:"saved"}];nativeExportUI.draft={...exportDraft(),profile_id:"p"}');
 await b.run('exportEnsurePolicy()');assert.equal(b.run('nativeExportUI.draft.profile_name'),'Saved');assert.equal(b.run('nativeExportUI.draft.editProfileID'),'p');
 b.run('nativeExportUI.draft={...exportDraft(),profile_id:"missing"}');await b.run('exportEnsurePolicy()');
 assert.match(b.run('nativeExportUI.error'),/no longer exists/);assert.equal(b.run('nativeExportUI.draft.policy'),undefined);
});
test('named profile errors retain draft and pending YAML cannot be saved',async()=>{
 const b=browser();profileSetup(b);b.element('export-saved-profile-name').value='Kept';
 b.context.catalogPost=async()=>{throw Error('saved profiles changed; reload')};
 await b.run('exportSaveProfile()');assert.match(b.run('nativeExportUI.error'),/saved profiles changed/);
 assert.equal(b.run('nativeExportUI.draft.savedProfileName'),'Kept');assert.equal(b.run('nativeExportUI.draft.path'),'/output');
 b.run('nativeExportUI.draft.profileRaw="invalid"');await b.run('exportSaveProfile()');assert.match(b.run('nativeExportUI.error'),/Validate your YAML/);
});

function profileSetup(b){
 b.run('state.view="export";nativeExportUI.draft={...exportDraft(),name:"Keep destination",rom_root:"/roms",path:"/output",platform:"Console",profile_name:"Custom profile",policy:{schema_version:1,titles:{mode:"regional"},launchbox:{regions:["Japan"]},selection:{description:{languages:["en"],scope:"game",sources:["launchbox"]},fields:{title:{sources:["launchbox"]}},media:{}}}};nativeExportUI.profileOptions={available_sources:["launchbox","manual.notes"],media_roles:{"es-de":[{id:"cover",label:"Cover"}]}}');
 for(const [id,value]of Object.entries({'export-destination-name':'Keep destination','export-run-target':'es-de','export-destination-path':'/output','export-rom-root':'/roms','export-run-platform':'Console','export-media-root':'','export-rom-mode':'single'}))b.element(id).value=value;
}
test('inline profile preview opens a separate tab with exact policy and keeps setup intact',async()=>{
 const b=browser();profileSetup(b);const data={},destinations=[];
 b.context.window.open=(url,target)=>{assert.equal(url,'about:blank');assert.equal(target,'_blank');return{sessionStorage:{setItem:(k,v)=>data[k]=JSON.parse(v)},location:{replace:url=>destinations.push(url)},close(){throw Error('unexpected close')}}};
 b.context.catalogPost=async(path,body)=>{assert.equal(path,'/api/export/profile');assert.equal(body.profile.launchbox.regions[0],'Japan');return{profile_yaml:'schema_version: 1\nselection:\n  media: {}\n',target:'es-de'}};
 await b.run('exportPreviewProfile()');
 assert.equal(b.run('state.view'),'export');assert.equal(b.run('nativeExportUI.draft.rom_root'),'/roms');assert.equal(b.run('nativeExportUI.draft.path'),'/output');
 assert.equal(data['rommeta.export-preview'].profile_yaml,'schema_version: 1\nselection:\n  media: {}\n');assert.equal(data['rommeta.export-preview'].platform,'Console');assert.deepEqual(destinations,['/?export-preview=1']);
});
test('preview rejects invalid profile, closes only its reserved tab and keeps the draft',async()=>{
 const b=browser();profileSetup(b);let closed=false;
 b.context.window.open=()=>({close(){closed=true}});b.context.catalogPost=async()=>{throw Error('Invalid rule scope')};
 await b.run('exportPreviewProfile()');assert.equal(closed,true);assert.match(b.run('nativeExportUI.error'),/Invalid rule/);assert.equal(b.run('nativeExportUI.draft.path'),'/output');
});
test('late profile loading cannot overwrite a different saved destination',async()=>{
 const b=browser();let resolve;b.context.catalogPost=()=>new Promise(r=>resolve=r);
 b.run('nativeExportUI.draft=exportDraft()');const loading=b.run('exportEnsurePolicy()');b.run('nativeExportUI.draft={...exportDraft(),name:"Newer"}');
 resolve({profile:{titles:{mode:"release"}},profile_yaml:'old'});await loading;assert.equal(b.run('nativeExportUI.draft.policy'),undefined);assert.equal(b.run('nativeExportUI.draft.name'),'Newer');
});
test('unvalidated YAML is retained and prevents saving or planning a different policy',async()=>{
 const b=browser();profileSetup(b);b.run('nativeExportUI.draft.profileRaw="bad: ["');
 await assert.rejects(b.run('exportDestinationInput()'),/Validate your YAML/);assert.equal(b.run('nativeExportUI.draft.profileRaw'),'bad: [');assert.equal(b.calls.length,0);
});
test('profile form has direct fields and one separate-preview action',()=>{
 const b=browser();profileSetup(b);b.run('exportRender()');const html=b.element('native-export-pane').innerHTML;
 assert.match(html,/data-profile-field="titles.mode"/);assert.match(html,/data-profile-field="selection.description.languages"/);
 assert.equal((html.match(/data-export-preview/g)||[]).length,1);assert.doesNotMatch(html,/export-profile-mode|Edit browser profile|View effective profile/);
});



test('artwork preferences preserve custom rules, skip video/manual and restore loaded priorities',()=>{
 const b=browser();profileSetup(b);
 b.run('nativeExportUI.draft.policy.selection.media={cover:{scope:"release",order:["source","region","format"],sources:["launchbox"],styles:["scan"],formats:["png"],variants:[]},screenshot:{scope:"game",order:["source"],sources:["igdb"]},video:{order:["source"],sources:["emumovies"]},manual:{order:["source"],formats:["pdf"]}}');
 const before=b.run('JSON.stringify(nativeExportUI.draft.policy.selection.media)');
 b.run('exportChangeArtwork("reconstructed")');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.cover.variants[0]'),'reconstructed');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.screenshot.order.join(",")'),'region,variant,source,format');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.cover.styles[0]'),'scan');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.video.variants'),undefined);
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.manual.variants'),undefined);
 b.run('exportChangeArtwork("original")');assert.equal(b.run('nativeExportUI.draft.policy.selection.media.cover.variants[0]'),'original');
 b.run('exportChangeArtwork("auto")');assert.equal(b.run('JSON.stringify(nativeExportUI.draft.policy.selection.media)'),before);
 b.run('exportChangeArtwork("reconstructed");exportInstallPolicy(nativeExportUI.draft,{profile:nativeExportUI.draft.policy,profile_yaml:"saved"})');
 assert.equal(b.run('exportArtworkPreference(nativeExportUI.draft)'),'reconstructed');
 assert.equal(b.run('nativeExportUI.draft.artworkBase'),undefined);
});

test('newly enabled images inherit artwork preference and disabled roles keep their original rules',()=>{
 const b=browser();profileSetup(b);
 b.run('nativeExportUI.draft.policy.selection.media={cover:{scope:"release",order:["source","format"],sources:["launchbox"]}};nativeExportUI.profileOptions.artwork_sources=["igdb"];exportChangeArtwork("original")');
 let change;const control={dataset:{profileMedia:'cover'},checked:false,addEventListener:(event,fn)=>change=fn};
 b.context.document.querySelectorAll=selector=>selector==='[data-profile-media]'?[control]:[];
 b.run('exportBindProfile()');change();
 b.run('exportChangeArtwork("reconstructed")');control.checked=true;change();
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.cover.variants[0]'),'reconstructed');
 control.dataset.profileMedia='screenshot';change();
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.screenshot.variants[0]'),'reconstructed');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.screenshot.sources[0]'),'igdb');
 b.run('exportChangeArtwork("auto")');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.cover.order.join(",")'),'source,format');
 assert.equal(b.run('nativeExportUI.draft.policy.selection.media.screenshot.variants'),undefined);
});

test('Download YAML downloads validated current YAML without saving paths or preparing an export',async()=>{
 const b=browser();profileSetup(b);let payload,blob,clicked=false,revoked=false;
 b.context.Blob=Blob;b.context.URL={createObjectURL:value=>{blob=value;return 'blob:profile'},revokeObjectURL:url=>{assert.equal(url,'blob:profile');revoked=true}};
 b.context.setTimeout=fn=>fn();const link={click(){clicked=true}};
 b.context.document.createElement=name=>{assert.equal(name,'a');return link};
 b.context.catalogPost=async(path,body)=>{assert.equal(path,'/api/export/profile');payload=body;return{target:'es-de',profile_yaml:'schema_version: 1\n# validated policy\n'}};
 b.run('nativeExportUI.draft.policy.selection.media.cover={order:["source"],sources:["launchbox"]};exportChangeArtwork("original")');await b.run('exportDownloadProfile()');
 assert.equal(clicked,true);assert.equal(link.download,'rommeta-es-de-export.yaml');
 assert.equal(await blob.text(),'schema_version: 1\n# validated policy\n');assert.equal(revoked,true);
 assert.equal(payload.profile.selection.media.cover.variants[0],'original');assert.equal(payload.path,undefined);
 assert.equal(b.run('nativeExportUI.draft.path'),'/output');assert.equal(b.run('state.view'),'export');
 assert.equal(b.element('export-profile-download').disabled,false);
});

test('Download YAML rejects pending or invalid YAML without a download and keeps the draft',async()=>{
 const b=browser();profileSetup(b);let downloads=0,requests=0;
 b.context.document.createElement=()=>{downloads++;return{click(){}}};
 b.context.catalogPost=async()=>{requests++;throw Error('Invalid profile')};
 b.run('nativeExportUI.draft.profileRaw="bad: ["');await b.run('exportDownloadProfile()');
 assert.equal(requests,0);assert.equal(downloads,0);assert.match(b.run('nativeExportUI.error'),/Validate your YAML/);
 b.run('delete nativeExportUI.draft.profileRaw');await b.run('exportDownloadProfile()');
 assert.equal(requests,1);assert.equal(downloads,0);assert.equal(b.run('nativeExportUI.error'),'Invalid profile');
 assert.equal(b.run('nativeExportUI.draft.path'),'/output');
});

test('typed profile languages are captured before save without a blur event',async()=>{
 const b=browser();profileSetup(b);const listeners={};
 b.context.document.querySelectorAll=selector=>selector==='[data-profile-field]'?[{tagName:'INPUT',dataset:{profileField:'selection.description.languages'},value:'ja, en',addEventListener:(type,fn)=>listeners[type]=fn}]:[];
 b.run('exportBindProfile()');assert.equal(typeof listeners.input,'function');listeners.input();
 assert.equal(b.run('nativeExportUI.draft.policy.selection.description.languages.join(",")'),'ja,en');
 b.context.catalogPost=async(path,body)=>({profile_yaml:body.profile.selection.description.languages.join(','),profile:body.profile});
 const result=await b.run('exportDestinationInput()');assert.equal(result.profile_yaml,'ja,en');assert.equal(result.path,'/output');
});

test('raw YAML edits disable structured profile controls until validated or discarded',()=>{
 const b=browser();profileSetup(b);const control={disabled:false},note=b.element('export-profile-pending');
 b.context.document.querySelectorAll=selector=>selector.includes('[data-profile-field],')?[control]:[];
 b.run('nativeExportUI.draft.profileRaw="bad: [";exportProfilePending()');assert.equal(control.disabled,true);assert.equal(note.hidden,false);
 b.run('delete nativeExportUI.draft.profileRaw;exportProfilePending()');assert.equal(control.disabled,false);assert.equal(note.hidden,true);
});

test('growing media discovery does not imply an almost finished download task',()=>{
 const b=browser();b.context.progress={Stage:'files',Transfers:{Queued:126,Downloaded:125,Failed:0,Existing:0,Bytes:1048576,DiscoveryCurrent:18,DiscoveryTotal:228,DiscoveryStage:'scanning',DiscoveryComplete:false}};
 const html=b.run('enrichmentProgress({enrichment:{jobs:[{source:"screenscraper",type:"artwork"}],current:0,progress}})');
 assert.match(html,/18 \/ 228 games/);assert.match(html,/125 files processed/);assert.match(html,/126 files found to download so far/);
 assert.doesNotMatch(html,/not known yet|still checking|Final file count|max="126"|99%/);
});
test('finished discovery exposes final file totals and distinguishes failures and existing files',()=>{
 const b=browser();b.context.progress={Stage:'files',Transfers:{Queued:200,Downloaded:125,Failed:2,Existing:10,Bytes:1024,DiscoveryComplete:true}};
 const html=b.run('enrichmentProgress({enrichment:{jobs:[],progress}})');
 assert.match(html,/127 \/ 200 files processed · 63%/);assert.match(html,/125 downloaded · 10 already present · 2 failed · 73 pending/);
 assert.match(html,/value="127" max="200"/);assert.doesNotMatch(html,/not known|so far/);
});
test('archive and file processing use their own counts instead of completed transfers',()=>{
 const b=browser();
 for(const [Stage,unit] of [['extracting','packages'],['indexing files','packages'],['materializing','files'],['reading catalog','games'],['scanning catalog','platforms']]){
  b.context.progress={Stage,Current:2,Total:7,Transfers:{Queued:30,Downloaded:30,Failed:0,Existing:0,DiscoveryComplete:true}};
  const html=b.run('enrichmentProgress({enrichment:{jobs:[],progress}})');
  assert.match(html,new RegExp('2 / 7 '+unit));assert.match(html,/value="2" max="7"/);assert.doesNotMatch(html,/value="30"|max="30"/);
 }
});
test('known and unknown byte downloads retain visible byte counters for snapshots and workers',()=>{
 const b=browser();
 b.context.progress={Stage:'downloading',BytesCurrent:1024,BytesTotal:4096};
 let html=b.run('enrichmentProgress({enrichment:{jobs:[],progress}})');
 assert.match(html,/1 KiB \/ 4 KiB received · 25%/);assert.match(html,/value="1024" max="4096"/);
 b.context.progress.BytesTotal=0;
 html=b.run('enrichmentProgress({enrichment:{jobs:[],progress,workers:{1:{Stage:"downloading",Item:"file",BytesCurrent:2048,BytesTotal:0}}}})');
 assert.match(html,/1 KiB received/);assert.match(html,/2 KiB received/);assert.doesNotMatch(html,/value=/);
});
test('empty known queue and all-existing media never invent a pending download',()=>{
 const b=browser();b.context.progress={Stage:'files',Transfers:{Queued:0,Downloaded:0,Failed:0,Existing:12,DiscoveryComplete:true}};
 const html=b.run('enrichmentProgress({enrichment:{jobs:[],progress}})');
 assert.match(html,/0 \/ 0 files processed/);assert.match(html,/12 already present · 0 failed · 0 pending/);assert.doesNotMatch(html,/not known/);
});
test('DAT and native export stages show counts with their actual units',()=>{
 const b=browser();
 for(const [Stage,unit] of [['writing','cards'],['checking existing','cards'],['bundling','releases']]){
  b.context.progress={Stage,Current:4,Total:9};
  assert.match(b.run('operationDetail({state:"importing",progress})'),new RegExp('4 / 9 '+unit));
 }
 for(const [Stage,unit] of [['write','files'],['n64 / ROMs','ROM files'],['n64 / metadata/media','ROM entries'],['index','cards']]){
  b.context.progress={Stage,Current:4,Total:9};
  assert.match(b.run('nativeExportDetail({state:"planning",export:{destination:{},progress}})'),new RegExp('4 / 9 '+unit));
 }
 b.context.progress={Stage:'ROMs',Current:25,Total:0};
 assert.match(b.run('nativeExportDetail({state:"planning",export:{destination:{},progress}})'),/25 ROM files/);
});
test('upload progress reports bytes and only uses a percentage for a known total',()=>{
 const b=browser();b.run('libraryUI.uploadProgress={loaded:1024,total:4096}');
 assert.match(b.run('libraryUploadStatus()'),/1 KiB \/ 4 KiB · 25%/);
 b.run('libraryUI.uploadProgress.total=0');
 assert.match(b.run('libraryUploadStatus()'),/1 KiB sent/);assert.doesNotMatch(b.run('libraryUploadStatus()'),/%/);
});

test('Retry is offered for any failed source stage, including earlier stages of an active batch',()=>{
 const b=browser();
 for(const type of ['metadata','artwork','video'])for(const state of ['failed','incomplete','skipped']){
  b.context.task={source:'provider',type,state};
  assert.match(b.run('enrichmentRetryAction({id:"parent"},task,2)'),/data-enrichment-retry-task="2"/);
 }
 for(const state of ['pending','running','succeeded']){
  b.context.task={state};assert.equal(b.run('enrichmentRetryAction({},task,0)'),'');
 }
 b.run('catalogUI.enabled=false');
 assert.equal(b.run('enrichmentRetryAction({},{state:"failed"},0)'),'');
});
test('Retry sends only the selected task and switches to the server-owned queued operation',async()=>{
 const b=browser(),child={id:'retry',kind:'enrichment',state:'queued',platform:'Console',enrichment:{jobs:[],platforms:['Console']}};
 b.context.parent={id:'parent',enrichment:{jobs:[{state:'succeeded'},{state:'failed',source:'screenscraper',type:'artwork'}]}};
 b.context.catalogPost=async(path,body)=>{b.calls.push(['POST',path,body]);return child};
 b.context.fetchJSON=async()=>[child];
 await b.run('enrichmentRetryTask(parent,1)');
 assert.equal(b.calls[0][1],'/api/catalog/enrichment-retry');assert.equal(b.calls[0][2].task,1);assert.equal(b.calls[0][2].id,'parent');
 assert.equal(b.run('libraryUI.selected'),'retry');assert.equal(b.run('parent.enrichment.jobs[1].retry_operation'),'retry');
 assert.match(b.run('enrichmentRetryAction(parent,parent.enrichment.jobs[1],1)'),/View retry/);
 assert.doesNotMatch(b.run('enrichmentRetryAction(parent,parent.enrichment.jobs[1],1)'),/data-enrichment-retry-task/);
});
test('failed import and export return to review instead of replaying writes',()=>{
 const b=browser();
 assert.match(b.run('operationDetail({state:"failed",summary:{},progress:{}})'),/Retry — prepare plan/);
 for(const state of ['failed','incomplete']){
  b.context.failedState=state;
  const html=b.run('nativeExportDetail({state:failedState,export:{destination:{}}})');
  assert.match(html,/Retry — review a new plan/);assert.doesNotMatch(html,/data-native-export-apply/);
 }
});

test('retry setup errors remain attached to the stage after progress polling',async()=>{
 const b=browser();
 b.context.parent={id:'parent',kind:'enrichment',state:'incomplete',enrichment:{jobs:[{state:'failed',source:'provider',type:'video'}]}};
 b.context.catalogPost=async()=>{throw new Error("Credentials are missing")};
 b.context.fetchJSON=async()=>[b.context.parent];
 await b.run('enrichmentRetryTask(parent,0)');
 await b.run('libraryPollOperations()');
 assert.match(b.run('enrichmentRetryAction(parent,parent.enrichment.jobs[0],0)'),/Credentials are missing/);
});

test('manual export choice clears exclusion, stays a draft and is restored from Operations',()=>{
 const b=browser();b.run('libraryRenderOperations=()=>{};const job={id:"job",revision:3,state:"ready",kind:"export",export:{excluded:["input"],totals:{issues:[{id:"input",rom:"Unclear.z64",status:"unmatched",can_bind:true}]}}};libraryUI.jobs=[job];nativeExportUI.match={job,id:"input",target:{game:"platforms/n64/Chosen",game_revision:"digest"},release:"Chosen (USA)"};exportUseMatch()');
 assert.equal(b.run('exportIssueDraft(job).excluded.size'),0);
 assert.equal(b.run('exportReviewDirty(job)'),true);
 assert.equal(b.calls.length,0);
 const html=b.run('exportIssues(job)');
 assert.match(html,/Manual choice/);assert.match(html,/Change game/);assert.match(html,/Use automatic match/);assert.match(html,/Update the plan/);
 b.run('const choices=[...exportIssueDraft(job).bindings.values()];job.revision++;job.export.excluded=[];job.export.bindings=choices;nativeExportUI.reviewDrafts.clear()');
 assert.equal(b.run('exportReviewDirty(job)'),false);
 assert.equal(b.run('exportIssueDraft(job).bindings.get("input").release'),'Chosen (USA)');
 b.run('exportIssueDraft(job).bindings.clear()');
 assert.equal(b.run('exportReviewDirty(job)'),true);
});
test('manual picker escapes card data and shows release choices, never offers unsupported inputs',()=>{
 const b=browser();
 b.run('nativeExportUI.match={job:{},id:"input",rom:"<bad>",query:"",target:{title:"<script>",game:"platforms/x/<game>",releases:["<edition>"]},release:""};exportRenderMatch()');
 const html=b.element('export-match-dialog').innerHTML;
 assert.match(html,/&lt;script>/);assert.match(html,/&lt;edition>/);assert.doesNotMatch(html,/<script>/);assert.match(html,/Game only/);assert.match(html,/not a fingerprint match/);
 const rows=b.run('exportIssues({id:"x",revision:1,state:"ready",export:{totals:{issues:[{id:"unsupported",rom:"bad.zip",status:"unsupported",can_bind:false}]}}})');
 assert.doesNotMatch(rows,/data-export-match=/);
});
test('late manual search cannot replace a newer selection or reopen a closed picker',async()=>{
 const b=browser();b.run('const replies=[];fetchJSON=()=>new Promise(resolve=>replies.push(resolve));nativeExportUI.match={job:{id:"j",revision:1},id:"i",query:"",offset:0,games:[],total:0};const first=exportLoadMatch();const second=exportLoadMatch("platforms/x/Chosen")');
 b.run('replies[1]({target:{title:"Chosen",game:"platforms/x/Chosen",releases:[],game_revision:"hash"}})');await b.run('second');
 b.run('replies[0]({games:[{name:"Old",game:"old"}],total:1})');await b.run('first');
 assert.equal(b.run('nativeExportUI.match.target.title'),'Chosen');
 b.run('const pending=exportLoadMatch();exportCloseMatch();replies[2]({games:[],total:0})');await b.run('pending');
 assert.equal(b.run('nativeExportUI.match'),null);assert.equal(b.element('export-match-dialog').classes.has('hidden'),true);
});
test('changed export revision cannot accept a stale manual selection',()=>{
 const b=browser();b.run('const old={id:"j",revision:1,state:"ready",export:{}};libraryUI.jobs=[{...old,revision:2}];nativeExportUI.match={job:old,id:"i",rom:"file",target:{title:"Chosen",releases:[],game:"platforms/x/Chosen",game_revision:"hash"},query:"",release:""};exportUseMatch()');
 assert.match(b.run('nativeExportUI.match.error'),/Plan changed/);assert.equal(b.run('nativeExportUI.reviewDrafts.size'),0);assert.equal(b.calls.length,0);
});
test('review polling picks up committed bindings after planning at the same revision',()=>{
 const b=browser();b.run('const job={id:"j",revision:2,state:"planning",export:{totals:{issues:[]}}};exportIssues(job);job.state="ready";job.export.bindings=[{id:"input",game:"platforms/n64/Chosen",game_revision:"digest"}];exportIssues(job)');
 assert.equal(b.run('exportIssueDraft(job).bindings.size'),1);assert.equal(b.run('exportReviewDirty(job)'),false);
});

test('finished operation close buttons dismiss the selected result through the shared action',async()=>{
 for(const kind of ['dat-import','enrichment','export']){
  for(const state of ['succeeded','incomplete','failed','ready','planning','importing','enriching','exporting','queued']){
   const b=browser();b.context.fixture={id:'chosen',revision:7,kind,state,filename:'Example.dat',platform:'Console',summary:{platform:'Console'},enrichment:{jobs:[],platforms:['Console']},export:{destination:{name:'Example'},written:0}};
   const html=b.run('operationDetail(fixture)'),finished=['succeeded','incomplete','failed'].includes(state);
   assert.equal(html.includes('aria-label="Dismiss result from Operations"'),finished,kind+' '+state);
   if(finished)assert.match(html,/Saved files stay in place/);
  }
 }
 for(const index of [0,1]){
  const b=browser(),buttons=[b.element('close-result'),b.element('dismiss-result')];
  b.element('operations-pane').querySelectorAll=selector=>selector==='[data-import-dismiss]'?buttons:[];
  b.run('libraryUI.jobs=[{id:"chosen",revision:7,kind:"export",state:"succeeded",export:{destination:{name:"Example"}}}];libraryUI.selected="chosen";catalogPost=async(path,body)=>{request={path,body};return {dismissed:true}};libraryRenderOperations()');
  await buttons[index].listeners.click();
  assert.deepEqual(JSON.parse(b.run('JSON.stringify(request)')),{path:'/api/operations/dismiss',body:{id:'chosen',revision:7}});
  assert.equal(b.run('libraryUI.jobs.length'),0);
 }
});

test('completed import offers Fetch metadata for its resolved platform without starting a job',async()=>{
 const b=browser(),button=b.element('fetch-imported');
 b.element('operations-pane').querySelectorAll=selector=>selector==='[data-import-fetch]'?[button]:[];
 b.context.fetchJSON=async path=>{b.calls.push(['GET',path]);return path==='/api/operations'?b.run('libraryUI.jobs'):path==='/api/platforms'?b.run('state.platforms'):{revision:'settings',choices:[{source:'launchbox',type:'metadata',available:true,default:true}]}};
 b.run('state.platform="Other";state.platforms=[{platform:"Imported console"},{platform:"Other"}];libraryUI.jobs=[{id:"done",kind:"dat-import",state:"succeeded",revision:2,filename:"example.dat",platform:"Original input",summary:{platform:"Imported console"}}];libraryUI.selected="done";libraryRenderOperations()');
 assert.match(b.element('operations-pane').innerHTML,/Next step:/);assert.match(b.element('operations-pane').innerHTML,/data-import-fetch="done"/);
 await button.listeners.click();
 assert.equal(b.run('state.view'),'operations');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(enrichmentUI.draft.platforms)')),['Imported console']);
 assert.equal(b.run('enrichmentUI.draft.selected.has("launchbox:metadata")'),true);
 assert.equal(b.calls.filter(x=>x[0]==='POST').length,0);
 for(const status of ['ready','planning','importing','failed']){
  b.run('libraryUI.jobs[0].state='+JSON.stringify(status));
  assert.doesNotMatch(b.run('operationDetail(libraryUI.jobs[0])'),/data-import-fetch/);
 }
 b.run('libraryUI.jobs[0].state="succeeded";catalogUI.enabled=false');
 assert.doesNotMatch(b.run('operationDetail(libraryUI.jobs[0])'),/data-import-fetch/);
});
