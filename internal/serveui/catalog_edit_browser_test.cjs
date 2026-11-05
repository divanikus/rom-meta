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
 vm.runInContext(readFileSync(require("node:path").join(__dirname,"static/catalog-register.js"),"utf8"),context);
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

test('edit mode exposes every field and names the exact destination',()=>{
 const b=browser(async()=>json({}));b.run('catalogUI.enabled=true;Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",card:{fields:{title:"Orbit"},records:[],registry:{sources:[]}}})');
 const html=b.run('catalogRenderEditor({}, "")');
 for(const field of ['title','developer','publisher','release_date','genres','players'])assert.match(html,new RegExp('data-card-field="'+field+'"'));
 assert.match(b.run('catalogInlineBanner()'),/Card fields/);
 b.run('catalogInline.target={kind:"source",source:"launchbox",match:1}');
 assert.match(b.run('catalogInlineBanner()'),/launchbox · editing its saved values/);
});
test('the toolbar can finish editing while a card is loading or failed to load',async()=>{
 for(const failed of [false,true]){
  const b=browser(async()=>json({}));
  b.run('catalogUI.enabled=true;fetchJSON=()=>new Promise((resolve,reject)=>{state.resolveEdit=resolve;state.rejectEdit=reject})');
  const pending=b.run('catalogStartInline()');
  if(failed){b.run('state.rejectEdit(new Error("Unavailable"))');await pending;}
  assert.equal((b.run('catalogInlineBanner()').match(/>Done</g)||[]).length,1);
  let finish;b.element('catalog-inline-cancel').addEventListener=(type,listener)=>{if(type==='click')finish=listener};
  b.run('catalogBindInline()');finish();
  assert.equal(b.run('catalogInline.mode'),false);
  if(!failed){b.run('state.resolveEdit({fields:{title:"Late response"}})');await pending;}
  assert.equal(b.run('catalogInline.card'),null);
 }
});
test('save commits drafts for multiple records and stays in edit mode',async()=>{
 let sent;const card={revision:'new',fields:{title:'Orbit'},profile:{title:'Orbit'},records:[],registry:{sources:[]}};
 const b=browser(async(path,options)=>{sent={path,body:JSON.parse(options.body)};return json({card});});
 b.run('Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",card:{revision:"old",fields:{title:"Orbit"},records:[]}});catalogEditorChange("developer","My studio");catalogInline.target={kind:"source",source:"launchbox",match:1};catalogEditorChange("publisher","Corrected publisher");state.game={game:{title:"Orbit"},source_blocks:[{source:"launchbox"}]}');
 await b.run('catalogSaveInline()');
 assert.equal(sent.body.edits.length,2);assert.deepEqual(sent.body.edits[1],{target:{kind:'source',source:'launchbox',match:1},fields:{publisher:'Corrected publisher'}});
 assert.equal(sent.body.revision,'old');assert.equal(b.run('catalogInline.mode'),true);assert.equal(b.run('catalogInline.dirty'),false);
 assert.equal(b.run('catalogInline.card.revision'),'new');
});
test('failed saves retain all drafts and block navigation only during the request',async()=>{
 let resolve;const b=browser(()=>new Promise(r=>resolve=r));
 b.run('Object.assign(catalogInline,{platform:"Console",folder:"One",card:{revision:"old",fields:{}}});catalogEditorChange("developer","New")');
 const pending=b.run('catalogSaveInline()');assert.equal(b.run('catalogLeaveInline()'),false);
 resolve({ok:false,json:async()=>({error:'Card changed; reload before saving'})});await pending;
 assert.equal(b.run('catalogTargetValues().developer'),'New');assert.equal(b.run('catalogInline.dirty'),true);assert.match(b.run('catalogInline.error'),/Card changed/);
 assert.equal(b.run('catalogInline.saving'),false);
});
test('language changes preserve other languages and reverting a value clears its draft',()=>{
 const b=browser(async()=>json({}));b.run('catalogInline.card={fields:{descriptions:{en:"Saved English",ru:"Saved Russian"}}};catalogEditorDescription("en","New English")');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogTargetValues().descriptions)')),{en:'New English',ru:'Saved Russian'});
 b.run('catalogEditorDescription("en","")');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogTargetValues().descriptions)')),{ru:'Saved Russian'});
 b.run('catalogEditorDescription("en","Saved English")');assert.equal(b.run('catalogInline.dirty'),false);
});
test('invalid structured values survive rendering and keep save disabled',()=>{
 const b=browser(async()=>json({}));b.run('catalogInline.card={fields:{}};catalogInline.target={kind:"source",source:"launchbox",match:0};catalogInline.invalid.set(catalogTargetKey(catalogInline.target)+"|media","{broken");catalogEditorChange("developer","Draft")');
 assert.match(b.run('catalogEditorControl("media",{cover:"old"})'),/\{broken/);
 assert.equal(b.element('catalog-inline-save').disabled,true);
});
test('late editor reads cannot open a different game',async()=>{
 const b=browser(async()=>json({}));b.run('catalogUI.enabled=true;fetchJSON=()=>new Promise(r=>state.resolveEdit=r)');
 const pending=b.run('catalogStartInline("developer")');
 b.run('catalogLeaveInline();state.selectedFolder="Two";state.resolveEdit({fields:{},profile:{}})');
 await pending;assert.equal(b.run('catalogInline.mode'),false);assert.equal(b.run('catalogInline.card'),null);
});
test('saved card remains saved if refreshing export choices fails',async()=>{
 const b=browser(async()=>json({card:{revision:'new',fields:{title:'Orbit'},profile:{}}}));
 b.run('Object.assign(catalogInline,{platform:"Console",folder:"One",card:{revision:"old",fields:{}}});catalogEditorChange("developer","New");fetchJSON=async()=>{throw Error("Offline")};state.game={game:{title:"Orbit"}}');
 await b.run('catalogSaveInline()');assert.equal(b.run('catalogInline.dirty'),false);assert.match(b.run('catalogInline.message'),/Saved.*Could not refresh/);
});
test('creating a named variant selects it without copying fields or enabling it',async()=>{
 let sent;const b=browser(async(path,options)=>{sent=JSON.parse(options.body);return json({source:{id:'manual.one',name:'Personal'},registry:{revision:'new',sources:[{id:'manual.one',name:'Personal'}]}})});
 b.run('catalogInline.card={fields:{developer:"Provider"},registry:{revision:"old",sources:[]}};catalogInline.nameMode="create";$("catalog-source-name").value="Personal";state.sourceDefaults={metadata:["launchbox"]};fetchJSON=async()=>({metadata:["launchbox"],available_metadata:["launchbox","manual.one"]})');
 await b.run('catalogSaveSourceName()');assert.deepEqual(sent,{revision:'old',id:'',name:'Personal'});
 assert.equal(b.run('catalogInline.target.source'),'manual.one');assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogTargetValues())')),{});
 assert.equal(b.run('catalogInline.dirty'),false);assert.equal(b.run('state.sourceDefaults.metadata.join(",")'),'launchbox');
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
 assert.match(b.element('catalog-subtitle').textContent,/One batch will save these changes/);
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
test('an untouched new release record remains the visible edit destination',()=>{
 const b=browser(async()=>json({}));b.run('catalogInline.card={fields:{},registry:{sources:[{id:"manual.one",name:"Personal"}]},releases:["USA","Japan"],records:[{target:{kind:"source",source:"manual.one",match:0},fields:{developer:"Whole game"}}]};catalogInline.target={kind:"manual",source:"manual.one",match:-1,scope:"Japan"}');
 assert.equal(b.run('catalogEditorTargets().filter(r=>catalogTargetKey(r.target)===catalogTargetKey(catalogInline.target)).length'),1);
 assert.equal(b.run('catalogScopeExists("")'),true);assert.equal(b.run('catalogScopeExists("Japan")'),false);
 b.run('catalogInline.saving=true;catalogEditorChange("developer","Late draft")');assert.equal(b.run('catalogInline.edits.size'),0);
});

test('source navigation shows names once and keeps each record draft',()=>{
 const b=browser(async()=>json({}));b.run('catalogUI.enabled=true;Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",card:{fields:{title:"Orbit"},registry:{sources:[{id:"manual.one",name:"Personal"}]},records:[{target:{kind:"source",source:"launchbox",match:-1},fields:{status:"matched"},label:"Source details"},{target:{kind:"source",source:"launchbox",match:0},fields:{title:"Provider"},label:"Record 1"},{target:{kind:"source",source:"launchbox",match:1},fields:{title:"Other"},label:"Record 2"}]}});catalogSelectSource("launchbox");catalogEditorChange("title","Corrected");catalogSelectSource("manual.one");catalogEditorChange("title","Personal title");catalogSelectSource("launchbox")');
 assert.equal(b.run('catalogInline.target.match'),0);assert.equal(b.run('catalogTargetValues().title'),'Corrected');
 assert.equal(b.run('catalogInline.edits.size'),2);
 const html=b.run('catalogInlineBanner()');assert.equal((html.match(/data-editor-source="launchbox"/g)||[]).length,1);
 assert.match(html,/Entry<select/);assert.doesNotMatch(html,/>Editing<select/);
});
test('form to raw to form saves against original revision with all drafts',async()=>{
 const requests=[];const card={revision:'draft',yaml:'title: Draft\n',fields:{title:'Draft'},records:[],registry:{sources:[]}};
 const b=browser(async(path,options)=>{requests.push({path,body:JSON.parse(options.body)});return json(path.endsWith('edit-preview')?card:{card:{...card,revision:'saved'},saved:true});});
 b.run('catalogUI.enabled=true;Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",baseRevision:"original",card:{revision:"original",yaml:"title: Orbit\\n",fields:{title:"Orbit"},records:[],registry:{sources:[]}}});catalogEditorChange("title","Draft")');
 assert.equal(await b.run('catalogMaterializeDraft()'),true);
 assert.equal(b.run('catalogInline.card.revision'),'draft');assert.equal(b.run('catalogInline.baseRevision'),'original');
 b.run('catalogEditorChange("developer","Studio")');await b.run('catalogSaveInline()');
 assert.equal(requests[1].body.revision,'original');assert.equal(requests[1].body.edits[0].target.kind,'raw');assert.equal(requests[1].body.edits[1].fields.developer,'Studio');
 assert.equal(b.run('catalogInline.dirty'),false);assert.equal(b.run('catalogInline.raw'),null);
});
test('invalid raw keeps the draft and stays on Raw until corrected',async()=>{
 const b=browser(async()=>({ok:false,json:async()=>({error:'line 3: duplicate title'})}));
 b.run('catalogUI.enabled=true;state.detailTab="raw";Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",dirty:true,raw:"broken YAML",baseRevision:"original",card:{revision:"original",fields:{},registry:{sources:[]}}})');
 assert.equal(await b.run('catalogSwitchDetailTab("preview")'),false);
 assert.equal(b.run('catalogInline.raw'),'broken YAML');assert.equal(b.run('catalogInline.baseRevision'),'original');
 assert.match(b.run('catalogInline.error'),/line 3: duplicate title/);
 assert.equal(b.run('catalogInline.saving'),false);assert.equal(b.run('catalogInline.dirty'),true);
});

test('Raw materialization selects the newly created manual record for further form edits',async()=>{
 const target={kind:'source',source:'manual.one',match:0};
 const card={revision:'draft',yaml:'title: Orbit\n',fields:{title:'Orbit'},registry:{sources:[{id:'manual.one',name:'Personal'}]},records:[{target,fields:{title:'Personal title'}}]};
 const b=browser(async()=>json(card));
 b.run('Object.assign(catalogInline,{mode:true,platform:"Console",folder:"One",baseRevision:"saved",card:{revision:"saved",fields:{},registry:{sources:[]}},target:{kind:"manual",source:"manual.one",match:-1,scope:""}});catalogEditorChange("title","Personal title")');
 await b.run('catalogMaterializeDraft()');
 assert.equal(b.run('catalogInline.target.kind'),'source');assert.equal(b.run('catalogTargetValues().title'),'Personal title');
 b.run('catalogEditorChange("title","Revised")');
 assert.equal(b.run('catalogEditorEdits()[1].target.kind'),'source');assert.equal(b.run('catalogEditorEdits()[1].fields.title'),'Revised');
});

test('batch fold candidates and cached covers stay scoped to their platform',async()=>{
 const b=browser(async()=>json({}));b.run('catalogUI.platform="Console";catalogUI.platforms=["Console","Other"];state.reads=[];fetchJSON=async path=>{state.reads.push(path);return {game:{folder:"One"},fields:{},profile:{}}}');
 b.run('a={id:0,group:{Platform:"Console",Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}};z={id:1,group:{Platform:"Other",Keep:{Folder:"One"},Merge:[{Folder:"Two"}]}};');
 await b.run('catalogCandidates(a);');await b.run('catalogCandidates(z);');
 assert.equal(b.run('state.reads.length'),8);assert.equal(b.run('state.reads.filter(x=>x.includes("platform=Other")).length'),4);
 assert.notEqual(b.run('catalogGroupKey(a)'),b.run('catalogGroupKey(z)'));
 b.run('catalogUI.selected=new Set([0,1]);catalogUI.plan={groups:[{...a,status:"ready"},{...z,status:"ready"}]};catalogRenderPlan()');
 assert.match(b.element('catalog-content').innerHTML,/data-plan-platform="Other"/);
 assert.match(b.element('catalog-content').innerHTML,/catalog-platform-label">Other/);
});

test('discard restores saved values while retaining the selected source',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogInlineRender=()=>{};catalogInline.savedCard={fields:{title:"Orbit"},registry:{sources:[]},records:[{target:{kind:"source",source:"launchbox",match:0},fields:{title:"Provider"}}]};catalogInline.card=catalogInline.savedCard;catalogInline.target={kind:"source",source:"launchbox",match:0};catalogEditorChange("title","Draft");catalogInline.extra.add("publisher");catalogDiscardInline()');
 assert.equal(b.run('catalogInline.target.source'),'launchbox');
 assert.equal(b.run('catalogTargetValues().title'),'Provider');
 assert.equal(b.run('catalogInline.dirty'),false);assert.equal(b.run('catalogInline.extra.size'),0);
 b.run('catalogInline.target={kind:"source",source:"launchbox",match:99};catalogDiscardInline()');
 assert.equal(b.run('catalogInline.target.match'),0);
});

test('opening an editor uses displayed source values without copying them to library overrides',async()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.enabled=true;state.game={profile:{source:"launchbox",developer:"Studio"}};state.editorCard={fields:{title:"Orbit"},registry:{sources:[]},records:[{target:{kind:"source",source:"launchbox",match:0},fields:{title:"Orbit",developer:"Studio",descriptions:{en:"Text"}}}]};fetchJSON=async()=>state.editorCard;');
 await b.run('catalogStartInline()');
 assert.equal(b.run('catalogInline.target.source'),'launchbox');
 assert.equal(b.run('catalogTargetValues().developer'),'Studio');
 assert.equal(b.run('catalogInline.dirty'),false);
 assert.equal(b.run('catalogInline.edits.size'),0);
 assert.equal(b.run('catalogInitialTarget(state.editorCard,"description").source'),'launchbox');
 b.run('state.editorCard.records.push({target:{kind:"source",source:"other",match:0},fields:{developer:"Different"}});state.game.profile.developer="Different"');
 assert.equal(b.run('catalogInitialTarget(state.editorCard,"developer").source'),'other');
 b.run('catalogEditorChange("developer","Changed")');
 assert.equal(b.run('catalogEditorEdits()[0].target.source'),'launchbox');
 b.run('catalogLeaveInline()');await b.run('catalogStartInline("title")');
 assert.equal(b.run('catalogInline.target.kind'),'library');
 b.run('catalogLeaveInline();state.editorCard.fields.developer="Override"');await b.run('catalogStartInline("developer")');
 assert.equal(b.run('catalogInline.target.kind'),'library');
});

test('removing a translation stays scoped to its source and supports removing the last language',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogInline.card={fields:{descriptions:{en:"Library"}},records:[{target:{kind:"source",source:"ss",match:0},fields:{descriptions:{en:"English",ru:"Russian"}}}]};catalogInline.target={kind:"source",source:"ss",match:0};catalogInline.extra.add("ru");catalogRemoveDescription("ru")');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(catalogTargetValues().descriptions)')),{en:'English'});
 assert.equal(b.run('catalogInline.extra.has("ru")'),false);
 assert.equal(b.run('catalogInline.card.fields.descriptions.en'),'Library');
 assert.equal(b.run('catalogInline.card.records[0].fields.descriptions.ru'),'Russian');
 let html=b.run('catalogRenderEditor({},"")');assert.match(html,/data-description-remove="en"/);assert.doesNotMatch(html,/data-card-description="ru"/);
 b.run('catalogRemoveDescription("en")');
 assert.equal(b.run('[...catalogInline.edits.values()][0].fields.descriptions'),null);
 html=b.run('catalogRenderEditor({},"")');assert.doesNotMatch(html,/data-card-description=/);assert.match(html,/catalog-translation-tools" open/);
 b.run('catalogDiscardInline()');
 assert.equal(b.run('catalogTargetValues().descriptions.ru'),'Russian');assert.equal(b.run('catalogInline.dirty'),false);
});
test('removing an empty added translation creates no edit, and cannot change an in-flight save',()=>{
 const b=browser(async()=>json({}));b.run('catalogInline.card={fields:{}};catalogInline.extra.add("en");catalogRemoveDescription("en")');
 assert.equal(b.run('catalogInline.dirty'),false);assert.equal(b.run('catalogInline.extra.size'),0);
 b.run('catalogInline.card.fields.descriptions={en:"Saved"};catalogInline.saving=true;catalogRemoveDescription("en")');
 assert.equal(b.run('catalogTargetValues().descriptions.en'),'Saved');assert.equal(b.run('catalogInline.dirty'),false);
});
test('Compare includes the open card once and respects platform and unsaved-edit boundaries',async()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.comparePlatform="Console";catalogUI.compare=new Set(["Other"]);catalogBuildPlan=async manual=>{state.planned=manual;state.cards=[...catalogUI.compare]};catalogUpdateTools()');
 assert.equal(b.element('catalog-compare').disabled,false);assert.equal(b.element('catalog-compare').textContent,'Compare for merge (2)');
 await b.run('catalogCompareCurrent()');
 assert.deepEqual(JSON.parse(b.run('JSON.stringify(state.cards)')),['Other','One']);assert.equal(b.run('state.planned'),true);
 await b.run('catalogCompareCurrent()');assert.equal(b.run('state.cards.length'),2);
 b.run('state.planned=false;state.selectedFolder="Third";catalogLeaveInline=()=>false');
 await b.run('catalogCompareCurrent()');assert.equal(b.run('state.planned'),false);assert.equal(b.run('catalogUI.compare.has("Third")'),false);
 b.run('catalogLeaveInline=()=>true;state.platform="Different"');
 await b.run('catalogCompareCurrent()');assert.equal(b.run('state.planned'),false);assert.equal(b.run('catalogUI.compare.size'),0);
});
test('manual card selection opens its comparison directly without applying it',async()=>{
 const calls=[];const b=browser(async(path,options)=>{calls.push({path,body:JSON.parse(options.body)});return json({});});
 b.run('catalogUI.compare=new Set(["One","Two"]);catalogReadPlan=async()=>({id:"plan",groups:[{id:7,status:"review"}]});catalogReviewGroup=async id=>{state.reviewed=id;state.busyOnOpen=catalogUI.busy};catalogRenderPlan=()=>{state.board=true}');
 await b.run('catalogBuildPlan(true)');
 assert.equal(b.run('state.reviewed'),7);assert.equal(b.run('state.busyOnOpen'),false);assert.equal(b.run('state.board'),undefined);
 assert.deepEqual(calls.map(c=>c.path),['/api/catalog/fold-plan?stream=1']);assert.deepEqual(calls[0].body.games,['One','Two']);
});

test('closing a comparison restores its entry action for the same selection',async()=>{
 const b=browser(async()=>json({}));b.run('catalogUI.comparePlatform="Console";catalogUI.compare=new Set(["One","Two"]);catalogUI.busy=true;catalogUI.mode="review";catalogUpdateTools()');
 assert.equal(b.element('catalog-compare').disabled,true);
 await b.run('catalogClose()');
 assert.equal(b.element('catalog-compare').disabled,false);assert.equal(b.element('catalog-compare').textContent,'Compare for merge (2)');
});

test('release rename updates visible fingerprint/default references and all changes remain a draft',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogInline.mode=true;catalogInline.platform="Console";catalogInline.folder="One";catalogRegisterInstall({revision:"fp0",releases:[{name:"USA",fields:{title:"Orbit",custom:"keep"}}],fingerprints:[{index:0,fields:{release:"USA",file:{name:"orbit.bin",size:4,crc32:"1234abcd",custom:"keep"}}}],default_release:"USA"},true);catalogRegisterChange("release",0,"name","Europe");');
 const patch=JSON.parse(b.run('JSON.stringify(catalogRegisterPayload())'));
 assert.equal(patch.register.revision,'fp0');
 assert.equal(patch.register.patch.releases[0].original,'USA');
 assert.equal(patch.register.patch.fingerprints[0].fields.release,'Europe');
 assert.equal(patch.register.patch.default_release,'Europe');
 assert.equal(b.run('catalogRegister.fingerprints[0].fields.file.custom'),'keep');
 assert.equal(b.run('catalogInline.dirty'),true);
 b.run('catalogInline.card={fields:{},records:[],registry:{sources:[]}};catalogDiscardInline()');
 assert.equal(b.run('catalogRegister.releases[0].name'),'USA');
 assert.equal(b.run('catalogInline.dirty'),false);
});
test('removal is reversible and unsaved new records leave no deletion operation',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogRegisterInstall({revision:"fp0",releases:[],fingerprints:[]},true);catalogRegisterAdd("release");catalogRegisterChange("release",0,"name","USA");catalogRegisterAdd("fingerprint");catalogRegisterDelete("fingerprint",0);');
 assert.equal(b.run('catalogRegisterPatch().fingerprints'),undefined);
 b.run('catalogRegisterDelete("fingerprint",0)');
 assert.equal(b.run('catalogRegisterPatch().fingerprints[0].index'),-1);
});
test('register materialization carries fingerprint draft but keeps both original revision guards',async()=>{
 let request;
 const b=browser(async(path,options)=>{request=JSON.parse(options.body);return json({revision:"gameDraft",yaml:"title: Orbit\n",fields:{title:"Orbit"},records:[],register:{revision:"fpDraft",yaml:"items: []\n",releases:[{name:"Europe",fields:{}}],fingerprints:[],default_release:"Europe"}})});
 b.run('catalogInline.card={revision:"game0"};catalogInline.baseRevision="game0";catalogRegisterInstall({revision:"fp0",releases:[{name:"USA",fields:{}}],fingerprints:[],default_release:"USA"},true);catalogRegisterChange("release",0,"name","Europe")');
 assert.equal(await b.run('catalogMaterializeDraft()'),true);
 assert.equal(request.register.revision,'fp0');
 assert.equal(b.run('catalogInline.baseRevision'),'game0');
 assert.equal(b.run('catalogRegisterPayload().register.revision'),'fp0');
 assert.equal(b.run('catalogRegisterPayload().register.yaml'),'items: []\n');
 b.run('catalogRegisterChange("release",0,"name","World")');
 assert.equal(b.run('catalogRegisterPatch().releases[0].original'),'Europe');
});
test('register paging keeps unrendered drafts and rejects invalid file sizes',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogRegisterInstall({revision:"fp",releases:[],fingerprints:Array.from({length:42},(_,index)=>({index,fields:{file:{name:"File "+index,size:1,crc32:"12345678"}}}))},true);catalogRegisterChange("fingerprint",41,"file.name","Last edited");catalogRegister.page=2;');
 assert.match(b.run('catalogRenderRegister()'),/Last edited/);
 assert.doesNotMatch(b.run('catalogRenderRegister()'),/>File 0</);
 b.run('catalogRegisterChange("fingerprint",41,"file.size","1.5")');
 assert.equal(b.run('catalogInline.invalid.size'),1);
 b.run('catalogRegisterDelete("fingerprint",41)');
 assert.equal(b.run('catalogInline.invalid.size'),0);
});
test('release editing has its own compact context instead of provider tabs',()=>{
 const b=browser(async()=>json({}));
 b.run('catalogUI.enabled=true;catalogInline.mode=true;catalogInline.platform="Console";catalogInline.folder="One";state.detailTab="files";catalogInline.card={fields:{},records:[],registry:{sources:[]}}');
 const html=b.run('catalogInlineBanner()');
 assert.match(html,/Releases &amp; fingerprints|Releases & fingerprints/);
 assert.doesNotMatch(html,/catalog-source-tabs/);
});


test('saving release changes updates the game-list count and default immediately',async()=>{
 const card={revision:"next",yaml:"title: Orbit",fields:{title:"Orbit"},profile:{},records:[],register:{revision:"fpNext",yaml:"items: []",releases:[{name:"World",fields:{}}],fingerprints:[],default_release:"World"}};
 const b=browser(async()=>json({card,saved:true}));
 b.run('catalogUI.enabled=true;Object.assign(catalogInline,{mode:true,dirty:true,platform:"Console",folder:"One",card:{revision:"old"},baseRevision:"old"});state.games=[{folder:"One",title:"Orbit",release_count:2,default_release:"USA"}];fetchJSON=async()=>({})');
 await b.run('catalogSaveInline()');
 assert.equal(b.run('state.games[0].release_count'),1);
 assert.equal(b.run('state.games[0].default_release'),'World');
});
