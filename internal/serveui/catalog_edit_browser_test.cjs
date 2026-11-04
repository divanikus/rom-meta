const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const script=readFileSync(require('node:path').join(__dirname,'static/catalog-edit.js'),'utf8').replace(/catalogCapabilities\(\);\s*$/,'');
function browser(fetch){
 const elements=new Map();
 function element(id){if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',disabled:true,open:true,checked:false,value:'',parentElement:{},classList:{toggle(){}},addEventListener(){},setAttribute(k,v){this[k]=k==='disabled'?true:v;},close(){this.open=false;},showModal(){this.open=true;},replaceChildren(){}});return elements.get(id);}
 const context=vm.createContext({fetch,AbortController,URLSearchParams,TextDecoder,console,MutationObserver:class{observe(){}},window:{confirm:()=>true},$:element,
 document:{querySelectorAll:()=>[],createTextNode:s=>s},escapeHTML:s=>String(s).replaceAll('&','&amp;').replaceAll('<','&lt;'),escapeAttr:s=>String(s),
 state:{platform:'Console',games:[],game:{},selectedFolder:'One',view:'browse'},renderGames(){},selectGame:async()=>{},selectPlatform(){},fetchJSON:async()=>({}),bestCover:()=>null,renderReleases:()=>'',renderSourceSection:()=>'',renderMediaSection:()=>''});
 vm.runInContext(script,context);return{run:code=>vm.runInContext(code,context),element};
}
const json=value=>({ok:true,json:async()=>value});
test('a late preview cannot approve newer unsaved values',async()=>{
 let resolve;
 const b=browser(()=>new Promise(r=>resolve=r));
 b.run('catalogUI.version=1;catalogUI.patch={description:"Earlier"}');
 const preview=b.run('catalogPreviewEdit()');
 b.run('catalogUI.patch.description="Newer";catalogDirty()');
 resolve(json({fields:{description:'Earlier'},profile:{}}));await preview;
 assert.equal(b.run('catalogUI.approvedPatch'),null);
 assert.equal(b.element('catalog-commit').disabled,true);
});
test('closing the workspace invalidates an in-flight edit preview',async()=>{
 let resolve;const b=browser(()=>new Promise(r=>resolve=r));
 b.run('catalogUI.version=1;catalogUI.patch={description:"Draft"}');
 const preview=b.run('catalogPreviewEdit()');
 await b.run('catalogClose()');resolve(json({fields:{},profile:{}}));await preview;
 assert.equal(b.run('catalogUI.approvedPatch'),undefined);
 assert.equal(b.element('catalog-commit').disabled,true);
});
test('saving sends the exact approved patch and refreshes the library title',async()=>{
 let sent;const b=browser(async(path,options)=>{
  sent=JSON.parse(options.body);return json({card:{revision:'new',fields:{title:'New title'}}});
 });
 b.run('catalogUI.platform="Console";catalogUI.folder="One";catalogUI.revision="old";catalogUI.approvedPatch={title:"New title"};catalogUI.patch={title:"Unapproved"};state.games=[{folder:"One",title:"Old title"}]');
 await b.run('catalogSaveEdit()');
 assert.deepEqual(sent.fields,{title:'New title'});
 assert.equal(b.run('state.games[0].title'),'New title');
});
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
 assert.match(b.element('catalog-content').innerHTML,/Orbit \(USA\) \+ Orbit \(Japan\)/);
 assert.match(b.element('catalog-content').innerHTML,/data-plan-select="1" disabled/);
 assert.equal(b.element('catalog-apply-ready').textContent,'Apply ready groups (1)');
});
