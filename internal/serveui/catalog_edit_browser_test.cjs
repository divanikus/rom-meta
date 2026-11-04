const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const script=readFileSync(require('node:path').join(__dirname,'static/catalog-edit.js'),'utf8').replace(/catalogCapabilities\(\);\s*$/,'');
function browser(fetch){
 const elements=new Map();
 function element(id){if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',disabled:true,open:true,checked:false,value:'',parentElement:{},classList:{toggle(){}},addEventListener(){},setAttribute(k,v){this[k]=k==='disabled'?true:v;},close(){this.open=false;},showModal(){this.open=true;},replaceChildren(){},insertAdjacentHTML(position,html){this.innerHTML+=html;}});return elements.get(id);}
 const context=vm.createContext({fetch,AbortController,URLSearchParams,TextDecoder,console,MutationObserver:class{observe(){}},window:{confirm:()=>true},$:element,
 document:{querySelectorAll:()=>[],createTextNode:s=>s},escapeHTML:s=>String(s).replaceAll('&','&amp;').replaceAll('<','&lt;'),escapeAttr:s=>String(s),
 state:{platform:'Console',games:[],game:{},selectedFolder:'One',view:'browse'},renderDetails(){},sourceLabel:s=>s,fact:(l,v)=>l+":"+v,renderGames(){},renderPlatforms(){},selectGame:async()=>{},selectPlatform(){},fetchJSON:async()=>({}),bestCover:()=>null,renderReleases:()=>'',renderSourceSection:()=>'',renderMediaSection:()=>''});
 vm.runInContext(script,context);return{run:code=>vm.runInContext(code,context),element};
}
const json=value=>({ok:true,json:async()=>value});
test('reopening a prepared group asks for its current draft without replacing it',async()=>{
 let sent;const group={id:0,revision:4,status:'ready',group:{Keep:{Folder:'One',Title:'Orbit'},Merge:[],Edits:{description:'Reviewed'}}};
 const b=browser(async(path,options)=>{sent=JSON.parse(options.body);return json({group,draft:{fields:{description:'Reviewed'}},preview:{fields:{description:'Reviewed'}}});});
 b.run('catalogUI.plan={id:"plan",groups:[{id:0,group:{Keep:{Folder:"One"},Merge:[]}}]};catalogUI.platform="Console";fetchJSON=async()=>({game:{folder:"One"},fields:{title:"Orbit"},profile:{}});catalogRenderReview=()=>{}');
 await b.run('catalogReviewGroup(0)');
 assert.deepEqual(sent,{plan:'plan',group:0});
 assert.equal(b.run('catalogUI.patch.description'),'Reviewed');
 assert.equal(b.run('catalogUI.group.revision'),4);
});
test('structured language and provider descriptions render as text',()=>{
 const b=browser(async()=>json({}));
 assert.equal(b.run('catalogFieldValue("descriptions",{descriptions:{en:"Orbit",ja:"軌道"}})'),'en: Orbit\n\nja: 軌道');
 assert.equal(b.run('catalogFieldValue("descriptions",{}, {descriptions:[{language:"en",source:"launchbox",text:"Orbit"}]})'),'en · launchbox · Orbit');
});
test('a plan selects only ready groups for batch apply',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.plan={platform:"Console",groups:[{id:0,status:"ready",group:{Keep:{Title:"Orbit",Folder:"Orbit (USA)"},Merge:[{Title:"Orbit",Folder:"Orbit (Japan)"}]}},{id:1,status:"review",group:{Keep:{Title:"Garden",Folder:"Garden"}}}]};catalogUI.selected=new Set([0]);catalogRenderPlan()');
 assert.match(b.element('catalog-content').innerHTML,/data-plan-cover="Orbit \(USA\)"/);
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/data-plan-select="1"/);
 assert.equal(b.element('catalog-apply-ready').textContent,'Review decisions (1)');
});

test('successful and partially completed folds refresh platform counts without changing selection',async()=>{
 for(const partial of [false,true]){
  const b=browser(async()=>partial?{ok:false,json:async()=>({error:'Later group failed',consumed:true,result:{applied:1,kept_separate:0}})}:json({result:{applied:1,kept_separate:0}}));
  b.run('catalogUI.plan={id:"plan"};catalogUI.platform="Console";state.platforms=[{platform:"Console",game_count:2}];fetchJSON=async(path)=>{state.refreshPath=path;return [{platform:"Console",game_count:1},{platform:"Other",game_count:7}]};renderPlatforms=()=>{$("platform-list").textContent=String(state.platforms.find(p=>p.platform===state.platform).game_count)};selectPlatform=p=>{state.reloadedPlatform=p}');
  await b.run('catalogApply([{id:0,revision:1,action:"merge"}])');
  assert.equal(b.run('state.refreshPath'),'/api/platforms');
  assert.equal(b.element('platform-list').textContent,'1');
  assert.equal(b.element('platform-count').textContent,'2');
  assert.equal(b.run('state.reloadedPlatform'),'Console');
  assert.equal(b.run('catalogUI.plan'),null);
 }
});
test('refresh failure reports saved changes rather than a failed merge',async()=>{
 const b=browser(async()=>json({result:{applied:1,kept_separate:0}}));
 b.run('catalogUI.plan={id:"plan"};catalogUI.platform="Console";fetchJSON=async()=>{throw new Error("Offline")}');
 await b.run('catalogApply([{id:0,revision:1,action:"merge"}])');
 assert.equal(b.element('catalog-title').textContent,'Changes saved');
 assert.match(b.element('catalog-content').innerHTML,/1 groups merged/);
 assert.match(b.element('catalog-content').innerHTML,/Changes were saved, but the platform list could not refresh/);
});
test('hard conflicts do not offer field edits or identity confirmation as a solution',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.group={status:"blocked",reason:"conflicting saved fields: custom_note",conflicts:["custom_note"],needs_identity:true,group:{Keep:{Folder:"One"},Merge:[],Evidence:[]}};catalogUI.cards=[];catalogRenderReview()');
 const html=b.element('catalog-content').innerHTML;
 assert.match(html,/conflicting saved fields: custom_note/);
 assert.match(html,/Identity confirmation cannot discard conflicting data/);
 assert.match(html,/id="catalog-commit" disabled/);
 assert.match(html,/id="catalog-preview-group" disabled/);
 assert.match(b.element('catalog-subtitle').textContent,/Resolve the data conflict or change the selection/);
 assert.doesNotMatch(html,/Correct the result|catalog-confirm-identity|Result matches the current preview|Choose the saved values/);
});
test('editable-field and identity reviews retain their distinct guidance',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.group={status:"review",reason:"conflicting saved fields: description",conflicts:["description"],group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}],Evidence:[]}};catalogUI.cards=[];catalogRenderReview()');
 assert.match(b.element('catalog-content').innerHTML,/Choose the highlighted saved values/);
 assert.match(b.element('catalog-content').innerHTML,/Correct the result/);
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/id="catalog-preview-group" disabled/);
 assert.match(b.element('catalog-subtitle').textContent,/Needs your decision/);
 b.run('catalogUI.group.conflicts=[];catalogUI.group.needs_identity=true;catalogRenderReview()');
 assert.match(b.element('catalog-content').innerHTML,/Agree & next queues this selection/);
 assert.match(b.element('catalog-content').innerHTML,/Agree & next/);
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/cannot override the conflict/);
 b.run('catalogUI.group.group.ReviewAccepted=true;catalogUI.group.conflicts=["description"];catalogRenderReview()');
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/It is ready for batch apply/);
});

test('blocked-only plans explain that comparison is inspection rather than approval',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.plan={platform:"Console",groups:[{id:0,status:"blocked",reason:"different edition markers",group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}}]};catalogRenderPlan()');
 assert.match(b.element('catalog-content').innerHTML,/No groups are ready to merge/);
 assert.match(b.element('catalog-content').innerHTML,/it does not approve a merge/);
 assert.equal(b.element('catalog-apply-ready').disabled,true);
 b.run('catalogUI.plan.groups[0].status="ready";catalogUI.selected.add(0);catalogRenderPlan()');
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/No groups are ready to merge/);
 assert.equal(b.element('catalog-apply-ready').disabled,false);
});

test('owner confirmation queues the exact subset with the fresh ready revision',async()=>{
 const calls=[];
 const group={id:0,revision:7,status:'ready',group:{Keep:{Folder:'One'},Merge:[{Folder:'Two'}],ReviewAccepted:true}};
 const b=browser(async(path,options)=>{calls.push({path,body:JSON.parse(options.body)});return json({group,preview:{fields:{}}});});
 b.run('catalogUI.plan={id:"plan",groups:[{id:0}]};catalogUI.group={id:0,revision:3,status:"review",needs_identity:true,group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"},{Folder:"Three"}]}};catalogUI.included=new Set(["One","Two"]);catalogUI.selectionDirty=true;catalogRenderReview=()=>{};catalogQueueDecision=async action=>{state.queued={action,revision:catalogUI.group.revision}}');
 await b.run('catalogPreviewGroup({confirm:true,queue:true})');
 assert.deepEqual(calls[0].body.draft.folders,['One','Two']);
 assert.equal(calls[0].body.draft.confirm,true);
 assert.equal(b.run('state.queued.revision'),7);assert.equal(b.run('state.queued.action'),'merge');
 assert.equal(b.run('catalogUI.selected.has(0)'),true);
 assert.equal(b.run('catalogUI.selectionDirty'),false);
});
test('confirmation prepares batch selection without applying, and data conflicts still stop one-click merge',async()=>{
 for(const status of ['ready','blocked']){
  const group={id:0,revision:2,status,group:{Keep:{Folder:'One'},Merge:[{Folder:'Two'}],ReviewAccepted:true}};
  const b=browser(async()=>json({group,preview:{fields:{}}}));
  b.run('catalogUI.plan={id:"plan",groups:[{id:0}]};catalogUI.group={id:0,group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}};catalogRenderReview=()=>{};catalogApply=async()=>{state.applied=true}');
  await b.run('catalogPreviewGroup({confirm:true,queue:'+String(status==='blocked')+'})');
  assert.equal(b.run('state.applied'),undefined);
  assert.equal(b.run('catalogUI.selected.has(0)'),status==='ready');
 }
});
test('changing selection does not silently reuse an earlier owner confirmation',async()=>{
 let sent;const b=browser(async(path,options)=>{sent=JSON.parse(options.body);return json({group:{id:0,status:'review',group:{Keep:{Folder:'One'},Merge:[{Folder:'Three'}]}}});});
 b.run('catalogUI.plan={id:"plan",groups:[{id:0}]};catalogUI.group={id:0,group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}],ReviewAccepted:true}};catalogUI.included=new Set(["One","Three"]);catalogUI.selectionDirty=true;catalogRenderReview=()=>{}');
 await b.run('catalogPreviewGroup()');
 assert.equal(sent.draft.confirm,false);
 assert.deepEqual(sent.draft.folders,['One','Three']);
});
test('late confirmation cannot merge after the owner changes the selected cards',async()=>{
 let resolve;const b=browser(()=>new Promise(r=>resolve=r));
 b.run('catalogUI.version=1;catalogUI.plan={id:"plan",groups:[{id:0}]};catalogUI.group={id:0,group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}};catalogApply=async()=>{state.applied=true}');
 const request=b.run('catalogPreviewGroup({confirm:true,queue:true})');
 b.run('catalogUI.included=new Set(["One","Three"]);catalogUI.selectionDirty=true;catalogDirty()');
 resolve(json({group:{id:0,revision:2,status:'ready',group:{Keep:{Folder:'One'},Merge:[{Folder:'Two'}],ReviewAccepted:true}}}));await request;
 assert.equal(b.run('state.applied'),undefined);
 assert.equal(b.run('catalogUI.selectionDirty'),true);
 assert.equal(b.run('catalogUI.included.has("Three")'),true);
});

test('the board keeps three lanes and moves a confirmed review into the ready batch',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.plan={platform:"Console",groups:[{id:0,status:"ready",group:{Keep:{Folder:"Ready"},Merge:[{Folder:"Ready Alt"}]}},{id:1,status:"review",group:{Keep:{Folder:"Review"},Merge:[{Folder:"Review Alt"}]}},{id:2,status:"blocked",group:{Keep:{Folder:"Conflict"},Merge:[{Folder:"Conflict Alt"}]}}]};catalogUI.selected=new Set([0,2]);catalogRenderPlan()');
 const html=b.element('catalog-content').innerHTML;
 const lanes=html.split('<section class="catalog-lane ').slice(1);
 assert.equal(lanes.length,3);
 for(let id=0;id<3;id++){
  assert.match(lanes[id],new RegExp('data-plan-group="'+id+'"'));
  for(let other=0;other<3;other++)if(other!==id)assert.doesNotMatch(lanes[id],new RegExp('data-plan-group="'+other+'"'));
 }
 assert.equal(b.run('catalogUI.selected.has(2)'),false);
 b.run('catalogAcceptGroup({...catalogUI.plan.groups[1],status:"ready",revision:2});catalogRenderPlan()');
 assert.equal(b.element('catalog-apply-ready').textContent,'Review decisions (2)');
 assert.match(b.element('catalog-content').innerHTML,/Nothing needs your decision/);
 b.run('catalogSelectReady(false)');
 assert.equal(b.element('catalog-apply-ready').disabled,true);
 b.run('catalogSelectReady(true)');
 assert.deepEqual(Array.from(b.run('[...catalogUI.selected]')),[0,1]);
 assert.equal(b.element('catalog-select-ready').checked,true);
 b.run('catalogUI.selected.delete(0);catalogSelectionCount()');
 assert.equal(b.element('catalog-select-ready').indeterminate,true);
});

test('missing or inaccessible artwork leaves the plan usable and cached covers are reused',async()=>{
 const b=browser(async()=>json({}));
 b.run('state.slot={dataset:{planCover:"Orbit"},isConnected:true,replaceChildren(img){this.image=img}};state.cache=new Map();state.signal=new AbortController().signal;state.reads=0;fetchJSON=async()=>{state.reads++;return {local_media:[]}};bestCover=files=>files[0]||null');
 await b.run('catalogLoadPlanCover(state.slot,"Console",state.cache,state.signal)');
 assert.equal(b.run('state.slot.textContent'),'No cover');
 await b.run('catalogLoadPlanCover(state.slot,"Console",state.cache,state.signal)');
 assert.equal(b.run('state.reads'),1);
 b.run('state.cache.clear();fetchJSON=async()=>{throw new Error("unreadable")}');
 await b.run('catalogLoadPlanCover(state.slot,"Console",state.cache,state.signal)');
 assert.equal(b.run('state.slot.textContent'),'Cover unavailable');
 b.run('state.cache.clear();fetchJSON=async()=>({local_media:[{url:"/media?path=synthetic-cover.png"}]});document.createElement=()=>({addEventListener(kind,handler){this.onError=handler}})');
 await b.run('catalogLoadPlanCover(state.slot,"Console",state.cache,state.signal)');
 assert.equal(b.run('state.slot.image.src'),'/media?path=synthetic-cover.png');
 b.run('state.slot.image.onError()');
 assert.equal(b.run('state.slot.textContent'),'Cover unavailable');
});

test('board artwork loads only visible cards, limits concurrent reads and stops on leaving',async()=>{
 const b=browser(async()=>json({}));
 b.run('state.slots=Array.from({length:10},(_,i)=>({dataset:{planCover:"Card "+i},isConnected:true}));state.resolvers=[];state.reads=0;document.querySelectorAll=()=>state.slots;IntersectionObserver=class{constructor(callback){state.observe=callback}observe(){}unobserve(){}disconnect(){state.disconnected=true}};fetchJSON=()=>{state.reads++;return new Promise(resolve=>state.resolvers.push(resolve))};catalogLoadPlanArt()');
 assert.equal(b.run('state.reads'),0);
 b.run('state.observe(state.slots.map(target=>({target,isIntersecting:true})))');
 assert.equal(b.run('state.reads'),4);
 b.run('catalogStopPlanArt();state.resolvers.forEach(resolve=>resolve({local_media:[]}))');
 await new Promise(setImmediate);
 assert.equal(b.run('state.disconnected'),true);
 assert.equal(b.run('state.reads'),4);
 assert.equal(b.run('catalogUI.artwork.size'),0);
 assert.equal(b.run('state.slots[0].textContent'),undefined);
});

test('overview requests one primary cover and only single-action cards open comparison as a whole',()=>{
 const b=browser(async()=>json({}));
 for(const status of ['ready','review','blocked']){
  const html=b.run('catalogPlanCard({id:4,status:"'+status+'",group:{Keep:{Folder:"Orbit",Title:"Orbit"},Merge:[{Folder:"Orbit Alt"},{Folder:"Orbit JP"}]}})');
  assert.equal((html.match(/data-plan-cover=/g)||[]).length,1);
  assert.match(html,/data-plan-cover="Orbit"/);
  assert.doesNotMatch(html,/Orbit Alt|Orbit JP/);
  assert.equal((html.match(/data-plan-review="4"/g)||[]).length,1);
  if(status==='ready'){
   assert.match(html,/^<article /);
   assert.match(html,/data-plan-select="4"/);
   assert.match(html,/<button class="tab-button" data-plan-review="4">Compare<\/button>/);
  }else{
   assert.match(html,/^<button type="button"[^>]+aria-label="Compare Orbit">/);
   assert.doesNotMatch(html,/<input|<button[^>]*>Compare<\/button>/);
   assert.match(html,/3 cards to compare/);
  }
 }
});

test('batch apply keeps the board open, preserves unchecked groups and uses a fresh plan',async()=>{
 const group=(id,status,folder)=>({id,revision:1,status,group:{Keep:{Folder:folder,Title:folder},Merge:[{Folder:folder+'2'}]}});
 const next={id:'fresh',platform:'Console',groups:[group(0,'ready','Unchecked'),group(1,'review','Review')]};
 let request;
 const b=browser(async(path,options)=>{request=JSON.parse(options.body);return json({result:{applied:2,kept_separate:0},next_plan:next});});
 b.run('catalogUI.mode="plan";catalogUI.platform="Console";catalogUI.plan='+JSON.stringify({id:'old',groups:[group(2,'ready','Unchecked'),group(3,'ready','Merged')]})+';catalogUI.selected=new Set([3]);fetchJSON=async()=>[]');
 await b.run('catalogApply([{id:3,revision:1,action:"merge"}])');
 assert.equal(request.continue,true);assert.equal(b.run('catalogUI.plan.id'),'fresh');
 assert.equal(b.run('catalogUI.mode'),'plan');assert.equal(b.run('catalogUI.selected.size'),0);
 assert.match(b.element('catalog-content').innerHTML,/2 groups merged.*2 groups remaining/);
 assert.doesNotMatch(b.element('catalog-content').innerHTML,/All groups reviewed/);
});
test('remaining conflicts stay actionable and never appear as a completed review',async()=>{
 const b=browser(async()=>json({result:{applied:1,kept_separate:0},next_plan:{id:'next',platform:'Console',groups:[{id:0,status:'blocked',reason:'Unreadable media',group:{Keep:{Folder:'Blocked'},Merge:[]}}]}}));
 b.run('catalogUI.mode="review";catalogUI.platform="Console";catalogUI.plan={id:"old",groups:[]};fetchJSON=async()=>[];catalogReviewGroup=async id=>{catalogUI.mode="review";state.nextGroup=id}');
 await b.run('catalogApply([{id:0,revision:1,action:"merge"}])');
 assert.equal(b.run('catalogUI.mode'),'plan');assert.equal(b.run('catalogUI.plan.groups.length'),1);
 assert.notEqual(b.element('catalog-title').textContent,'Review complete');
});
test('continuation failure preserves the successful receipt and offers reload without replay',async()=>{
 const calls=[];
 const b=browser(async(path)=>{calls.push(path);return json({result:{applied:1,kept_separate:0,backups:['recovery']},consumed:true,continuation_error:'Catalog temporarily unavailable'});});
 b.run('catalogUI.mode="review";catalogUI.plan={id:"old",groups:[]};fetchJSON=async()=>[]');
 await b.run('catalogApply([{id:0,revision:1,action:"merge"}])');
 assert.equal(b.element('catalog-title').textContent,'Changes saved');
 assert.match(b.element('catalog-content').innerHTML,/1 groups merged.*recovery/s);
 assert.match(b.element('catalog-content').innerHTML,/Catalog temporarily unavailable/);
 assert.match(b.element('catalog-content').innerHTML,/Continue review/);
 assert.equal(calls.length,1);assert.equal(b.run('catalogUI.plan'),null);
});

test('inline save writes only the edited field, updates the card and preserves provider observations',async()=>{
 let sent;const provider={source:'launchbox',profile:{developer:'Provider studio'}};
 const b=browser(async(path,options)=>{sent={path,body:JSON.parse(options.body)};return json({card:{revision:'new',fields:{title:'Orbit',developer:'My studio'},profile:{title:'Orbit',developer:'My studio'},source_profile:{developer:'Provider studio'},yaml:'saved'}});});
 b.run('catalogInline.platform="Console";catalogInline.folder="One";catalogInline.card={revision:"old",fields:{title:"Orbit"}};catalogInline.field="developer";catalogInline.draft="My studio";catalogInline.dirty=true;state.game={game:{title:"Orbit"},source_blocks:'+JSON.stringify([provider])+'};state.games=[{folder:"One",title:"Orbit"}]');
 await b.run('catalogSaveInline()');
 assert.equal(sent.path,'/api/catalog/edit');assert.deepEqual(sent.body.fields,{developer:'My studio'});assert.equal(sent.body.revision,'old');
 assert.equal(b.run('state.game.profile.developer'),'My studio');assert.equal(b.run('state.game.source_blocks[0].profile.developer'),'Provider studio');
 assert.equal(b.run('catalogInline.field'),'');assert.equal(b.run('catalogInline.dirty'),false);
});
test('inline save retains a failed draft and blocks navigation only while saving',async()=>{
 let resolve;const b=browser(()=>new Promise(r=>resolve=r));
 b.run('catalogInline.platform="Console";catalogInline.folder="One";catalogInline.card={revision:"old",fields:{}};catalogInline.field="developer";catalogInline.draft="New";catalogInline.dirty=true');
 const pending=b.run('catalogSaveInline()');
 assert.equal(b.run('catalogLeaveInline()'),false);
 resolve({ok:false,json:async()=>({error:'Card changed; reload before saving'})});await pending;
 assert.equal(b.run('catalogInline.draft'),'New');assert.equal(b.run('catalogInline.dirty'),true);assert.match(b.run('catalogInline.error'),/Card changed/);
 assert.equal(b.run('catalogInline.saving'),false);
});
test('language-specific editing and source restoration preserve the other language corrections',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogInline.card={fields:{descriptions:{en:"Saved English",ru:"Saved Russian"}}};catalogInline.field="description";catalogInline.language="en";catalogInline.draft="New English"');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogInlinePatch())')),{descriptions:{en:'New English',ru:'Saved Russian'}});
 b.run('catalogInline.remove=true');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogInlinePatch())')),{descriptions:{ru:'Saved Russian'}});
 b.run('catalogInline.field="developer"');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogInlinePatch())')),{developer:null});
});
test('late inline reads never open an editor on a different game',async()=>{
 let resolve;const b=browser(async()=>json({}));
 b.run('catalogUI.enabled=true;fetchJSON=()=>new Promise(r=>state.resolveEdit=r)');
 const pending=b.run('catalogStartInline("developer")');
 b.run('catalogLeaveInline();state.selectedFolder="Two";state.resolveEdit({fields:{},profile:{}})');
 await pending;assert.equal(b.run('catalogInline.field'),'');
});
test('inline labels distinguish library title, stored values and source values',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.enabled=true;catalogInline.mode=true;state.game={catalog_fields:{title:"Orbit",developer:"Owner"},profile:{description:"Source",source:"igdb",description_language:"en"}}');
 assert.match(b.run('catalogInlineField("developer","Developer","Owner")'),/Saved in library/);
 assert.match(b.run('catalogInlineField("publisher","Publisher","Studio")'),/From sources/);
 assert.match(b.run('catalogInlineField("description","Description","Source")'),/igdb.*EN/);
 assert.match(b.run('catalogInlineBanner()'),/Release-specific values and export title settings/);
 b.run('catalogInline.platform="Console";catalogInline.folder="One";catalogInline.card={fields:{title:"Orbit"}};catalogInline.field="title"');
 assert.match(b.run('catalogInlineTitle("Orbit")'),/Exported names follow the release and title settings/);
 b.run('state.exportMode=true');
 assert.doesNotMatch(b.run('catalogInlineTitle("Orbit")'),/input|button/);
});

test('merge, separate and skip decisions advance without applying until the final batch',async()=>{
 const calls=[];const b=browser(async(path,options)=>{calls.push({path,body:JSON.parse(options.body)});return json({result:{applied:2,kept_separate:1},next_plan:{id:'fresh',platform:'Console',groups:[]}});});
 const group=(id,status)=>({id,revision:2,status,group:{Keep:{Folder:'Game'+id},Merge:[]}});
 b.run('catalogUI.plan='+JSON.stringify({id:'plan',platform:'Console',groups:[group(0,'ready'),group(1,'review'),group(2,'review'),group(3,'blocked')]})+';catalogUI.selected=new Set([0]);catalogUI.group=catalogUI.plan.groups[1];catalogReviewGroup=async id=>{catalogUI.group=catalogUI.plan.groups.find(g=>g.id===id);state.nextGroup=id};fetchJSON=async()=>[]');
 b.run('catalogUI.group.status="ready"');await b.run('catalogQueueDecision("merge")');
 assert.equal(b.run('state.nextGroup'),2);assert.equal(calls.length,0);
 await b.run('catalogQueueDecision("separate")');
 assert.equal(b.run('state.nextGroup'),3);assert.equal(calls.length,0);
 await b.run('catalogQueueDecision("skip")');
 assert.equal(b.run('catalogUI.mode'),'decisions');assert.equal(calls.length,0);
 assert.match(b.element('catalog-content').innerHTML,/2 merge.*1 keep separate.*1 unresolved/s);
 assert.match(b.element('catalog-content').innerHTML,/Nothing saved yet/);
 await b.run('catalogApply(catalogBatchDecisions())');
 assert.equal(calls.filter(c=>c.path.endsWith('fold-apply')).length,1);
 assert.deepEqual(calls[0].body.decisions.map(d=>d.action).sort(),['merge','merge','separate']);
 assert.equal(b.run('catalogUI.decisions.size'),0);
});
test('deselecting prepared merges removes them from the final batch while keeping separation decisions',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.plan={groups:[{id:0,status:"ready",revision:2,group:{Keep:{Folder:"One"}}},{id:1,status:"review",revision:1,group:{Keep:{Folder:"Two"}}}]};catalogUI.selected=new Set([0]);catalogUI.decisions=new Map([[0,{id:0,revision:2,action:"merge"}],[1,{id:1,revision:1,action:"separate"}]]);catalogSelectReady(false)');
 assert.equal(b.run('catalogBatchDecisions().length'),1);assert.equal(b.run('catalogBatchDecisions()[0].action'),'separate');
});
test('queued decisions require confirmation before closing and can be retained',async()=>{
 const calls=[];const b=browser(async(path)=>{calls.push(path);return json({discarded:true});});
 b.run('catalogUI.plan={id:"plan"};catalogUI.decisions=new Map([[0,{id:0,action:"separate"}]]);window.confirm=()=>false');
 await b.run('catalogClose()');assert.equal(b.run('catalogUI.plan.id'),'plan');assert.equal(calls.length,0);
 b.run('window.confirm=()=>true');await b.run('catalogClose()');assert.equal(b.run('catalogUI.plan'),null);assert.equal(calls.length,1);
});
test('comparison cards are fetched once per review session and prefetch failures remain retryable',async()=>{
 const b=browser(async()=>json({}));
 b.run('state.reads=0;fetchJSON=async()=>{state.reads++;return {game:{folder:"One"},fields:{},profile:{}}};catalogUI.platform="Console";state.pg={id:0,group:{Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}}');
 await b.run('catalogCandidates(state.pg)');await b.run('catalogCandidates(state.pg)');
 assert.equal(b.run('state.reads'),4);
 b.run('catalogUI.cardsCache=new Map();fetchJSON=async()=>{throw new Error("Offline")}');
 await assert.rejects(b.run('catalogCandidates(state.pg)'),/Offline/);
 assert.equal(b.run('catalogUI.cardsCache.size'),0);
});

test('ready selection counters exclude queued separation and update the batch notice',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.plan={groups:[{id:0,status:"ready",revision:1},{id:1,status:"ready",revision:1}]};catalogUI.decisions=new Map([[1,{id:1,action:"separate"}]]);catalogSelectReady(true)');
 assert.equal(b.element('catalog-select-ready').checked,true);
 assert.match(b.element('catalog-queue-status').innerHTML,/1 to merge · 1 to keep separate/);
 b.run('catalogSelectReady(false)');
 assert.match(b.element('catalog-queue-status').innerHTML,/0 to merge · 1 to keep separate/);
});
test('starting fold review does not discard an unsaved inline correction',async()=>{
 const b=browser(async()=>{throw Error('Must not send a request');});
 b.run('catalogInline.dirty=true;window.confirm=()=>false');
 await b.run('catalogBuildPlan()');
 assert.equal(b.run('catalogInline.dirty'),true);
 assert.equal(b.run('catalogUI.busy'),false);
});
