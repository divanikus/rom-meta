
/* One page-session workspace: plans are prepared by Go, never inferred in JS. */
const catalogUI = {enabled:false, plan:null, selected:new Set(), compare:new Set(), controller:null, busy:false, dirty:false, mode:"", cards:[], included:null, selectionDirty:false, group:null, patch:{}, base:{}, revision:"", platform:"", folder:""};
const catalogLabels = {title:"Title",aliases:"Aliases",alternate_titles:"Alternate titles",description:"Description · all languages",descriptions:"Descriptions",release_date:"Release date",developer:"Developer",publisher:"Publisher",genres:"Genres",players:"Players"};
const catalogLists = new Set(["aliases","alternate_titles","genres"]);
const catalogLanguages = {en:"English",ru:"Русский",ja:"日本語",fr:"Français",de:"Deutsch",es:"Español",it:"Italiano",pt:"Português",zh:"中文",ko:"한국어"};
const catalogStatus = {ready:"Ready to apply",review:"Needs your decision",blocked:"Merge blocked"};
const cEscape = value => escapeHTML(String(value ?? ""));
async function catalogPost(path, body, signal) {
 const r = await fetch(path,{method:"POST",headers:{"Content-Type":"application/json","Accept":"application/json"},body:JSON.stringify(body),signal});
 const data = await r.json();
 if(!r.ok){const error=new Error(data.error || "The catalog operation could not finish.");error.consumed=data.consumed;error.result=data.result;throw error;}return data;
}
function catalogNotice(text,kind=""){return '<div class="catalog-notice '+kind+'" role="status">'+cEscape(text)+'</div>';}
function catalogHeader(title,subtitle){catalogStopPlanArt();$("catalog-dialog").classList.toggle("is-plan",catalogUI.mode==="plan");$("catalog-title").textContent=title;$("catalog-subtitle").textContent=subtitle||"";}
function catalogOpen(title,subtitle){
 catalogUI.version=(catalogUI.version||0)+1;catalogHeader(title,subtitle);const dialog=$("catalog-dialog");if(!dialog.open)dialog.showModal();$("catalog-content").innerHTML='<div class="catalog-progress"><span class="catalog-spinner"></span><span id="catalog-progress-text">Preparing your workspace…</span></div>';
}
function catalogShowError(error){if(error.name==="AbortError")return;const slot=$("catalog-feedback");if(slot)slot.innerHTML=catalogNotice(error.message,"error");else $("catalog-content").innerHTML=catalogNotice(error.message,"error");}
async function catalogClose(){
 if(catalogUI.busy&&(catalogUI.mode==="applying"||catalogUI.mode==="saving"))return;
 if(catalogUI.dirty&&!window.confirm("Discard unsaved changes?"))return;
 if(catalogUI.decisions?.size&&!window.confirm("Discard queued fold decisions? Nothing has been saved yet."))return;
 catalogStopPlanArt();catalogUI.artwork=new Map();catalogUI.cardsCache=new Map();catalogUI.decisions=new Map();catalogUI.version=(catalogUI.version||0)+1;catalogUI.controller?.abort();catalogUI.dirty=false;catalogUI.busy=false;
 if(catalogUI.plan){const id=catalogUI.plan.id;catalogUI.plan=null;catalogPost("/api/catalog/fold-discard",{plan:id}).catch(()=>{});}
 $("catalog-dialog").close();
}
function catalogUpdateTools(){
 if(catalogUI.comparePlatform&&catalogUI.comparePlatform!==state.platform){catalogUI.compare.clear();catalogUI.comparePlatform=state.platform;}
 $("catalog-add-compare").textContent=catalogUI.compare.has(state.selectedFolder)?"Remove from comparison":"Add to comparison";
 $("catalog-tools").classList.toggle("hidden",!catalogUI.enabled||state.view!=="browse"||state.exportMode);
 $("catalog-edit-card").disabled=!state.game;$("catalog-edit-card").textContent=catalogInline.mode?"✓ Finish editing":"✎ Edit card";$("catalog-build-plan").disabled=!state.platform;
 $("catalog-add-compare").disabled=!state.game;
 $("catalog-compare").disabled=catalogUI.compare.size<2;
 $("catalog-compare").textContent="Compare ("+catalogUI.compare.size+")";
 if(typeof librarySyncNavigation==="function")librarySyncNavigation();
}
async function catalogCapabilities(){
 try{const response=await fetchJSON("/api/catalog");catalogUI.enabled=!!response.editing;catalogUpdateTools();if(typeof renderDetails==="function")renderDetails();}catch{/* Read-only older servers remain browsable. */}
}
function catalogFieldValue(key, fields, profile={}) {
 const value=fields?.[key] ?? profile?.[key] ?? "";
 if(value&&typeof value==="object"&&!Array.isArray(value))return Object.entries(value).map(([k,v])=>k+": "+v).join("\n\n");
 return Array.isArray(value)?value.map(item=>item&&typeof item==="object"?[(item.language||""),(item.source||""),item.text||item.title||item.name||""].filter(Boolean).join(" · "):String(item)).join("\n\n"):String(value);
}
function catalogForm(base,profile={},conflicts=[]){
 const fields={...base,...catalogUI.patch};
 let html='<div class="catalog-form">';
 for(const key of Object.keys(catalogLabels)){
  if(key==="descriptions")continue;
  const value=fields[key]??"", list=catalogLists.has(key),text=Array.isArray(value)?value.join("\n"):String(value);
  const differs=conflicts.some(x=>x.split(".")[0]===key);
  html+='<label><span class="catalog-label"><span>'+cEscape(catalogLabels[key])+(differs?' · choose a value':'')+'</span>'+(key!=="title"?'<button type="button" class="catalog-reset" aria-label="Use sources for '+escapeAttr(catalogLabels[key])+'" data-edit-reset="'+key+'">Use sources</button>':'')+'</span>';
  const placeholder=catalogFieldValue(key,{},profile);
  html+=key==="description"||list?'<textarea aria-label="'+escapeAttr(catalogLabels[key])+'" data-edit-field="'+key+'" class="'+(key==="description"?"description":"")+'" placeholder="'+escapeAttr(placeholder)+'">'+cEscape(text)+'</textarea>':'<input aria-label="'+escapeAttr(catalogLabels[key])+'" data-edit-field="'+key+'" value="'+escapeAttr(text)+'" placeholder="'+escapeAttr(placeholder)+'">';
  html+=(list?'<small>One name per line</small>':!text&&placeholder?'<small>Currently from sources</small>':'')+'</label>';
 }
 const descriptions=fields.descriptions||{};
 html+='<fieldset style="border:1px solid var(--line);border-radius:10px;padding:12px"><legend>Descriptions by language</legend>';
 for(const [lang,text] of Object.entries(descriptions))html+='<label>'+cEscape(catalogLanguages[lang]||lang)+'<textarea class="description" data-edit-language="'+escapeAttr(lang)+'">'+cEscape(text)+'</textarea></label>';
 html+='<div class="catalog-actions"><select id="catalog-language" aria-label="Description language">'+Object.entries(catalogLanguages).map(([code,name])=>'<option value="'+code+'">'+name+'</option>').join("")+'</select><button class="tab-button" type="button" id="catalog-add-language">Add language</button></div></fieldset></div>';
 return html;
}
function catalogDirty(){catalogUI.version=(catalogUI.version||0)+1;catalogUI.approvedPatch=null;catalogUI.dirty=true;if(catalogUI.group){catalogUI.decisions?.delete(catalogUI.group.id);catalogUI.selected.delete(catalogUI.group.id);}$("catalog-commit")?.setAttribute("disabled","");const feedback=$("catalog-preview-feedback");if(feedback)feedback.textContent="Changes need a new preview.";}
function catalogBindForm(){
 document.querySelectorAll("[data-edit-field]").forEach(input=>input.addEventListener("input",()=>{
  const key=input.dataset.editField;catalogUI.patch[key]=catalogLists.has(key)?input.value.split("\n").map(x=>x.trim()).filter(Boolean):input.value||null;catalogDirty();
 }));
 document.querySelectorAll("[data-edit-language]").forEach(input=>input.addEventListener("input",()=>{
  const descriptions={...(catalogUI.patch.descriptions??catalogUI.base.descriptions??{})};
  if(input.value.trim())descriptions[input.dataset.editLanguage]=input.value;else delete descriptions[input.dataset.editLanguage];
  catalogUI.patch.descriptions=descriptions;catalogDirty();
 }));
 document.querySelectorAll("[data-edit-reset]").forEach(button=>button.addEventListener("click",()=>{
  catalogUI.patch[button.dataset.editReset]=null;catalogDirty();catalogRenderForm();
 }));
 $("catalog-add-language")?.addEventListener("click",()=>{
  const descriptions={...(catalogUI.patch.descriptions??catalogUI.base.descriptions??{})};
  const lang=$("catalog-language").value;if(!(lang in descriptions))descriptions[lang]="";catalogUI.patch.descriptions=descriptions;catalogDirty();catalogRenderForm();
 });
}
function catalogRenderForm(){
 const host=$("catalog-form-host");if(!host)return;
 host.innerHTML=catalogForm(catalogUI.base,catalogUI.formProfile,catalogUI.group?.conflicts||[]);catalogBindForm();
}
function catalogProfilePreview(card,emptyMessage="Preview the result to inspect the merged card."){
 if(!card)return '<p class="secondary">'+cEscape(emptyMessage)+'</p>';
 const profile=card.profile||{};
 return '<h3>'+cEscape(profile.title||card.fields?.title||"Result")+'</h3>'+["developer","publisher","release_date","genres","players"].map(key=>{
  const value=catalogFieldValue(key,{},profile);return value?'<div class="catalog-field"><div class="catalog-field-label">'+cEscape(catalogLabels[key])+'</div><div class="catalog-field-value">'+cEscape(value)+'</div></div>':'';
 }).join("")+(profile.description?'<div class="catalog-field"><div class="catalog-field-label">Description'+(profile.description_language?' · '+cEscape(profile.description_language):'')+'</div><div class="catalog-field-value">'+cEscape(profile.description)+'</div></div>':'')+'<details class="raw-details"><summary>Catalog details</summary><pre>'+cEscape(card.yaml||"")+'</pre></details>';
}
async function catalogReadPlan(response){
 if(!response.ok){const data=await response.json();throw new Error(data.error||"Could not build a plan");}
 const reader=response.body.getReader(),decoder=new TextDecoder();let buffer="",result;
 const frame=line=>{if(!line.trim())return;const value=JSON.parse(line);if(value.error)throw new Error(value.error);if(value.result)result=value.result;if(value.progress){const p=value.progress;const el=$("catalog-progress-text");if(el)el.textContent=(p.Stage||"Preparing")+(p.Total?" · "+p.Current+" / "+p.Total:"")+(p.Item?" · "+p.Item:"");}};
 try{for(;;){const {value,done}=await reader.read();if(done)break;buffer+=decoder.decode(value,{stream:true});let end;while((end=buffer.indexOf("\n"))>=0){frame(buffer.slice(0,end));buffer=buffer.slice(end+1);}}buffer+=decoder.decode();frame(buffer);if(!result)throw new Error("Planning stopped before completion. Build a new plan.");return result;}finally{reader.releaseLock?.();}
}
async function catalogBuildPlan(manual=false,resume=false){
 if(!resume)catalogUI.completed=null;
 const platform=resume?catalogUI.platform:state.platform;
 if(!state.platform||catalogUI.busy)return;
 if(!resume&&!catalogLeaveInline())return;
 if(!resume){catalogInlineRender();catalogUpdateTools();}
 if(catalogUI.plan)await catalogPost("/api/catalog/fold-discard",{plan:catalogUI.plan.id}).catch(()=>{});
 catalogUI.mode="planning";catalogUI.busy=true;catalogUI.dirty=false;catalogUI.controller=new AbortController();catalogUI.platform=platform;
 catalogOpen(manual?"Compare selected games":"Find possible duplicates","Building a plan for "+platform+". Your catalog stays unchanged.");const version=catalogUI.version;
 try{
  const response=await fetch("/api/catalog/fold-plan?stream=1",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({platform,games:manual?[...catalogUI.compare]:[]}),signal:catalogUI.controller.signal});
  const plan=await catalogReadPlan(response);if(catalogUI.version!==version){catalogPost("/api/catalog/fold-discard",{plan:plan.id}).catch(()=>{});return;}catalogUI.plan=plan;catalogUI.artwork=new Map();catalogUI.cardsCache=new Map();catalogUI.decisions=new Map();catalogUI.selected=new Set(catalogUI.plan.groups.filter(g=>g.status==="ready").map(g=>g.id));
  if(resume&&!plan.groups.length)catalogFinishReview();else catalogRenderPlan();
 }catch(error){$("catalog-content").innerHTML=catalogSessionSummary()+catalogNotice(error.message,"error");if(resume)catalogResumeButton();}finally{catalogUI.busy=false;}
}
function catalogGroupReason(pg){if(pg.status==="review"&&pg.conflicts?.length)return "Choose the saved values to keep: "+[...new Set(pg.conflicts.map(key=>catalogLabels[key.split(".")[0]]||key))].join(", ")+ ".";return (pg.reason||"").replace("confirm regional titles, edition and content with --interactive","compare the regional titles, edition and content before confirming");}
function catalogIncluded(){return catalogUI.included||new Set([catalogUI.group.group.Keep,...(catalogUI.group.group.Merge||[])].map(c=>c.Folder));}
function catalogReviewHint(pg){
 if(pg.status==="blocked")return "The selected cards have a data or file conflict. Choose a different selection or resolve the reported problem before merging. Identity confirmation cannot discard conflicting data.";
 if(pg.conflicts?.length)return "Choose the highlighted saved values, then preview the result.";
 if(pg.needs_identity&&!pg.group.ReviewAccepted)return "Review the selected cards. Agree & next queues them for the final batch. Releases, source IDs and media are retained.";
 return "Preview the result to inspect the merged card.";
}
const catalogLanes = {
 ready:{title:"Ready to merge",hint:"Prepared automatically. Compare a few or apply the selected batch.",empty:"No groups are ready to merge yet."},
 review:{title:"Your review",hint:"Choose which cards belong together or which saved values to keep.",empty:"Nothing needs your decision."},
 blocked:{title:"Conflicts",hint:"Data or file problems need attention before these can merge.",empty:"No conflicts. There is nothing to fix here."}
};
function catalogPlanCard(pg){
 const g=pg.group,title=g.Keep.Title||g.Keep.Folder,count=1+(g.Merge||[]).length,ready=pg.status==="ready";
 const action=catalogUI.decisions?.get(pg.id)?.action;
 const reason=action==="skip"?"Skipped for now.":ready?(g.ReviewAccepted?"Agreed · queued for final apply.":""):catalogGroupReason(pg);
 const opening=ready?'<article class="catalog-plan-card" data-plan-group="'+pg.id+'">':'<button type="button" class="catalog-plan-card is-reviewable" data-plan-group="'+pg.id+'" data-plan-review="'+pg.id+'" aria-label="Compare '+escapeAttr(title)+'">';
 return opening+'<span class="catalog-plan-summary"><span class="catalog-plan-cover" data-plan-cover="'+escapeAttr(g.Keep.Folder)+'"><span>Cover</span></span>'+
 '<span class="catalog-plan-copy"><span class="catalog-plan-title">'+cEscape(title)+'</span><span class="catalog-card-count">'+count+' cards'+(ready?' → 1':' to compare')+'</span>'+
 (reason?'<span class="catalog-plan-reason" title="'+escapeAttr(reason)+'">'+cEscape(reason)+'</span>':'')+
 (!ready?'<span class="catalog-plan-open">Compare <span aria-hidden="true">→</span></span>':'')+'</span></span>'+
 (ready?'<div class="catalog-plan-card-actions"><label><input type="checkbox" aria-label="Include '+escapeAttr(title)+'" data-plan-select="'+pg.id+'" '+(catalogUI.selected.has(pg.id)?'checked':'')+'>Include</label><button class="tab-button" data-plan-review="'+pg.id+'">Compare</button></div></article>':'</button>');
}
function catalogRenderPlan(){
 catalogUI.mode="plan";catalogUI.group=null;catalogUI.dirty=false;
 const plan=catalogUI.plan;if(!plan)return;catalogHeader("Review duplicates",plan.platform+" · Compare cards, decide what belongs together, then apply.");
 const groups=plan.groups.filter(g=>catalogUI.decisions?.get(g.id)?.action!=="separate"),ready=groups.filter(g=>g.status==="ready");
 catalogUI.selected=new Set([...catalogUI.selected].filter(id=>ready.some(g=>g.id===id)));
 $("catalog-content").innerHTML=catalogSessionSummary()+catalogQueueNotice()+'<div class="catalog-queue-toolbar"><button class="catalog-primary" id="catalog-apply-ready">Review decisions</button></div><div id="catalog-feedback"></div>'+
 (!groups.length?catalogNotice("No duplicate groups found for this platform. You can also select cards in the library to compare them."):"")+
 (groups.length&&groups.every(g=>g.status==="blocked")?catalogNotice("No groups are ready to merge. Compare lets you inspect why each group is blocked; it does not approve a merge.","warn"):"")+
 '<p class="catalog-board-hint">Scroll sideways to see all three columns →</p><div class="catalog-board" tabindex="0" role="region" aria-label="Merge plan columns; scroll horizontally on narrow screens">'+Object.entries(catalogLanes).map(([status,lane])=>{
  const members=groups.filter(g=>g.status===status);
  return '<section class="catalog-lane '+status+'" aria-labelledby="catalog-lane-'+status+'"><header class="catalog-lane-head"><div><h3 id="catalog-lane-'+status+'">'+lane.title+'</h3><span class="catalog-lane-count">'+members.length+'</span></div><p>'+lane.hint+'</p></header>'+
  (status==="ready"?'<div class="catalog-batch"><label><input type="checkbox" id="catalog-select-ready">Select all</label><p id="catalog-selected-count"></p></div>':'')+
  '<div class="catalog-lane-list" tabindex="0" role="region" aria-label="'+lane.title+' groups">'+(members.length?members.map(catalogPlanCard).join(""):'<div class="catalog-lane-empty"><span aria-hidden="true">'+(status==="blocked"?"✓":status==="review"?"✓":"—")+'</span><p>'+lane.empty+'</p></div>')+'</div></section>';
 }).join("")+'</div>';
 document.querySelectorAll("[data-plan-select]").forEach(input=>input.addEventListener("change",()=>{const id=Number(input.dataset.planSelect);if(input.checked)catalogUI.selected.add(id);else {catalogUI.selected.delete(id);catalogUI.decisions?.delete(id);}catalogSelectionCount();}));
 document.querySelectorAll("[data-plan-review]").forEach(button=>button.addEventListener("click",()=>catalogReviewGroup(Number(button.dataset.planReview))));
 $("catalog-select-ready").addEventListener("change",event=>catalogSelectReady(event.target.checked));
 $("catalog-apply-ready").addEventListener("click",catalogRenderDecisions);
 catalogSelectionCount();catalogLoadPlanArt();catalogPrefetchNext();
}
function catalogSelectReady(checked){
 const ready=catalogUI.plan.groups.filter(g=>g.status==="ready"&&catalogUI.decisions?.get(g.id)?.action!=="separate");
 catalogUI.selected=new Set(checked?ready.map(g=>g.id):[]);
 for(const pg of ready)if(catalogUI.decisions?.get(pg.id)?.action==="merge"||(checked&&catalogUI.decisions?.get(pg.id)?.action==="skip"))catalogUI.decisions.delete(pg.id);
 document.querySelectorAll("[data-plan-select]").forEach(input=>{input.checked=catalogUI.selected.has(Number(input.dataset.planSelect));});
 catalogSelectionCount();
}
function catalogSelectionCount(){
 const n=catalogUI.selected.size,total=catalogUI.plan?.groups.filter(g=>g.status==="ready"&&catalogUI.decisions?.get(g.id)?.action!=="separate").length||0,button=$("catalog-apply-ready");
 if(button){const count=catalogBatchDecisions().length;button.textContent="Review decisions ("+count+")";button.disabled=count===0;}
 const all=$("catalog-select-ready");if(all){all.checked=total>0&&n===total;all.indeterminate=n>0&&n<total;all.disabled=total===0;}
 $("catalog-selected-count")?.replaceChildren(document.createTextNode(n+" of "+total+" ready groups selected"));
 const notice=$("catalog-queue-status");if(notice)notice.innerHTML=catalogQueueNoticeContent();
}
function catalogStopPlanArt(){
 catalogUI.artworkObserver?.disconnect();catalogUI.artworkController?.abort();
 catalogUI.artworkObserver=null;catalogUI.artworkController=null;
}
function catalogLoadPlanArt(){
 if(typeof IntersectionObserver==="undefined")return;
 const controller=new AbortController(),platform=catalogUI.platform,cache=catalogUI.artwork||(catalogUI.artwork=new Map()),queue=[];
 catalogUI.artworkController=controller;let active=0;
 const pump=()=>{
  while(active<4&&queue.length&&!controller.signal.aborted){
   const slot=queue.shift();active++;
   catalogLoadPlanCover(slot,platform,cache,controller.signal).finally(()=>{active--;pump();});
  }
 };
 const observer=new IntersectionObserver(entries=>{
  for(const entry of entries)if(entry.isIntersecting){observer.unobserve(entry.target);queue.push(entry.target);}
  pump();
 });
 catalogUI.artworkObserver=observer;
 document.querySelectorAll("[data-plan-cover]").forEach(slot=>observer.observe(slot));
}
async function catalogLoadPlanCover(slot,platform,cache,signal){
 const folder=slot.dataset.planCover,key=platform+"\n"+folder;
 try{
  let cover=cache.get(key);
  if(!cache.has(key)){
   const card=await fetchJSON("/api/game?"+new URLSearchParams({platform,game:folder}),signal);
   cover=bestCover(card.local_media||[]);if(signal.aborted)return;cache.set(key,cover);
  }
  if(signal.aborted||!slot.isConnected)return;
  if(!cover){slot.textContent="No cover";return;}
  const img=document.createElement("img");img.alt=folder;img.loading="lazy";img.decoding="async";
  img.addEventListener("error",()=>{if(slot.isConnected&&!signal.aborted)slot.textContent="Cover unavailable";});
  img.src=cover.url;slot.replaceChildren(img);
 }catch(error){if(!signal.aborted&&slot.isConnected)slot.textContent="Cover unavailable";}
}
function catalogCandidate(card,index,fields){
 const g=card.game,p=card.profile||{},snapshot=card.edit.fields,cover=bestCover(card.local_media||[]),included=catalogIncluded().has(g.folder),primary=included&&g.folder===catalogUI.group.group.Keep.Folder;
 return '<article class="catalog-candidate '+(primary?'is-primary':'')+(!included?' is-excluded':'')+'">'+(catalogUI.group.group.NativeDigest?'':'<label class="catalog-member"><input type="checkbox" data-candidate-include="'+index+'" '+(included?'checked':'')+'>Include in merge</label>')+(cover?'<img src="'+escapeAttr(cover.url)+'" alt="'+escapeAttr(p.title||g.folder)+'">':'')+'<span class="catalog-badge">'+(primary?"Suggested primary":"Candidate")+'</span><h3>'+cEscape(p.title||g.folder)+'</h3><p class="secondary">'+cEscape(g.folder)+'</p>'+
 (catalogUI.group.group.NativeDigest?'':'<button class="tab-button" data-candidate-primary="'+index+'" '+(primary||!included?'disabled':'')+'>Use as primary</button>')+
 fields.map(key=>{
  const values=catalogUI.cards.map(c=>catalogFieldValue(key,c.edit.fields,c.profile));const differs=new Set(values).size>1;const local=Object.prototype.hasOwnProperty.call(snapshot,key);
  return '<div class="catalog-field '+(differs?'different':'')+'"><div class="catalog-field-label"><span>'+cEscape(catalogLabels[key])+(differs?' · differs':'')+' · '+(local?'saved':'source')+'</span>'+(included&&local&&key!=="title"&&!catalogUI.group.group.NativeDigest&&catalogUI.group.status!=="blocked"?'<button class="tab-button" data-candidate-value="'+index+'" data-value-key="'+key+'">Use value</button>':'')+'</div><div class="catalog-field-value">'+cEscape(values[index]||"—")+'</div></div>';
 }).join("")+'<details><summary>Releases, media & evidence</summary>'+renderReleases(p.releases||g.releases||[])+'<p>'+cEscape((card.local_media||[]).length)+" local media files"+'</p><button class="tab-button" data-candidate-inspect="'+index+'">Inspect full card</button><pre>'+cEscape(card.catalog_yaml||"")+'</pre></details></article>';
}
function catalogNextGroup(exclude=[]){
 const groups=(catalogUI.plan?.groups||[]).filter(g=>!exclude.includes(g.id)&&!catalogUI.decisions?.has(g.id)&&g.status!=="ready");
 return groups.find(g=>g.status==="review")||groups.find(g=>g.status==="blocked")||groups[0];
}
function catalogCandidates(pg){
 const cache=catalogUI.cardsCache||(catalogUI.cardsCache=new Map()),key=catalogGroupKey(pg);
 if(cache.has(key))return cache.get(key).promise;
 const entry={cards:null};cache.set(key,entry);
 const platform=catalogUI.platform,refs=pg.candidates||[pg.group.Keep,...(pg.group.Merge||[])];
 entry.promise=Promise.all(refs.map(async ref=>{
  const query=new URLSearchParams({platform,game:ref.Folder});
  const [card,edit]=await Promise.all([fetchJSON("/api/game?"+query),fetchJSON("/api/catalog/edit?"+query)]);
  return {...card,edit};
 })).then(cards=>{entry.cards=cards;return cards;}).catch(error=>{cache.delete(key);throw error;});
 return entry.promise;
}
function catalogPrefetchNext(){
 const next=catalogNextGroup(catalogUI.group?[catalogUI.group.id]:[]);
 if(next)catalogCandidates(next).catch(()=>{});
}
function catalogUseReview(pg,cards,response={}){
 catalogUI.group=pg;catalogUI.cards=cards;catalogUI.primary="";catalogUI.patch=response.draft?.fields||pg.group.Edits||{};catalogUI.dirty=false;
 catalogUI.included=new Set([pg.group.Keep,...(pg.group.Merge||[])].map(c=>c.Folder));catalogUI.selectionDirty=false;catalogUI.preview=response.preview;
 const primary=cards.find(c=>c.game.folder===pg.group.Keep.Folder);
 catalogUI.base=pg.result?.fields||primary?.edit.fields||{};catalogUI.formProfile=response.preview?.profile||primary?.profile||{};
}
function catalogPending(message){
 document.querySelectorAll("#catalog-content button,#catalog-content input,#catalog-content textarea,#catalog-content select").forEach(el=>el.disabled=true);
 const slot=$("catalog-feedback");
 if(slot)slot.innerHTML=catalogNotice(message);else $("catalog-content").insertAdjacentHTML("afterbegin",catalogNotice(message));
}
async function catalogReviewGroup(id){
 if(catalogUI.busy)return;
 const group=catalogUI.plan.groups.find(g=>g.id===id);if(!group)return;
 catalogUI.busy=true;catalogUI.mode="review";catalogUI.version=(catalogUI.version||0)+1;const version=catalogUI.version;
 catalogPending("Opening comparison…");
 try{
  const cards=await catalogCandidates(group);
  if(catalogUI.version!==version)return;
  const response=await catalogPost("/api/catalog/fold-preview",{plan:catalogUI.plan.id,group:id});
  if(catalogUI.version!==version)return;
  catalogAcceptGroup(response.group);catalogUseReview(response.group,cards,response);
  catalogRenderReview();catalogPrefetchNext();
 }catch(error){if(catalogUI.version===version){catalogRenderPlan();catalogShowError(error);}}finally{catalogUI.busy=false;}
}
function catalogAcceptGroup(group){
 catalogUI.group=group;const i=catalogUI.plan.groups.findIndex(g=>g.id===group.id);catalogUI.plan.groups[i]=group;
 if(group.status==="ready")catalogUI.selected.add(group.id);else catalogUI.selected.delete(group.id);
}
function catalogRenderReview(){
 const pg=catalogUI.group,g=pg.group,blocked=pg.status==="blocked",included=catalogIncluded(),enough=included.size>=2,pendingIdentity=(pg.needs_identity&&!g.ReviewAccepted)||catalogUI.selectionDirty;
 const oldScroll=document.querySelector?.(".catalog-candidates");const scrollLeft=oldScroll?.scrollLeft||0,scrollTop=oldScroll?.scrollTop||0;
 catalogHeader("Compare games",included.size+" of "+catalogUI.cards.length+" cards selected · "+(catalogUI.selectionDirty?"Selection changed":catalogStatus[pg.status])+". "+(blocked&&!catalogUI.selectionDirty?"Resolve the data conflict or change the selection.":"Choose the cards to keep together. Unselected cards stay unchanged."));
 $("catalog-content").innerHTML=catalogSessionSummary()+catalogQueueNotice()+'<div class="catalog-queue-toolbar"><button class="tab-button" id="catalog-skip-group">Skip for now →</button><button class="tab-button" id="catalog-review-decisions">Review decisions</button></div><button class="catalog-back" id="catalog-back">← Back to plan</button><div id="catalog-feedback">'+(pg.reason?catalogNotice(catalogGroupReason(pg),pg.status==="blocked"?"error":"warn"):'')+'</div><div class="catalog-comparison"><section class="catalog-candidate-area"><p class="catalog-scroll-hint">Scroll sideways to compare cards →</p><div class="catalog-candidates" tabindex="0" role="region" aria-label="Candidate cards; scroll horizontally to compare">'+catalogUI.cards.map((card,i)=>catalogCandidate(card,i,["title","alternate_titles","description","descriptions","developer","publisher","release_date","genres","players"])).join("")+'</div></section><section class="catalog-result"><span class="catalog-eyebrow">'+(blocked?"Merge status":"Result")+'</span><h3>'+cEscape(blocked?"Merge blocked":g.Keep.Title||g.Keep.Folder)+'</h3>'+
 (blocked?'':g.NativeDigest?'<p>MAME chooses its primary from the accepted snapshot. Edit individual cards in the library before rebuilding this plan.</p>':'<details><summary>Correct the result</summary><div id="catalog-form-host"></div></details>')+
 (!blocked&&pendingIdentity?'<div class="catalog-notice warn">'+cEscape(pg.identity_reason||"Confirm that these cards belong together as one game.")+'</div><p class="secondary">Agree & next queues this selection. Changes are saved together after the final review. Releases, source IDs and media are retained.</p>':!blocked&&g.ReviewAccepted&&pg.status==="ready"?'<p class="secondary">This selection is ready for the final batch. Nothing has been saved yet.</p>':'')+
 '<div class="catalog-actions"><button class="tab-button" id="catalog-preview-group" '+(!enough||(blocked&&!catalogUI.selectionDirty)?'disabled':'')+'>'+(pendingIdentity?'Confirm selection':'Preview result')+'</button><button class="catalog-primary" id="catalog-commit" '+(!enough||(blocked&&!catalogUI.selectionDirty)||(pg.status==="review"&&!pendingIdentity&&!catalogUI.dirty)?'disabled':'')+'>'+'Agree & next'+'</button></div><p class="secondary" id="catalog-preview-feedback">'+(blocked?"Merge unavailable for this group.":catalogUI.dirty?"Changes need a new preview.":pg.status==="review"?"A decision is needed before applying.":"Result matches the current preview.")+'</p><div id="catalog-result-preview">'+catalogProfilePreview(catalogUI.preview,catalogReviewHint(pg))+'</div><details><summary>Why this group?</summary>'+(g.Evidence||[]).map(e=>'<p>'+cEscape(e.Source+": "+e.Field+" · "+e.Value)+'</p>').join("")+'</details><details><summary>Affected files</summary><pre>'+cEscape((g.FileChanges||[]).map(f=>f.Action+": "+f.FromFolder+"/"+f.Path+" → "+f.Target).join("\n")||"Catalog documents and merged card folders")+'</pre></details>'+
 '<div class="catalog-actions"><button class="tab-button" id="catalog-keep-separate" '+(!g.Evidence?.length||g.NativeDigest||catalogUI.dirty?'disabled':'')+'>Keep separate</button></div></section></div>';
 catalogRenderForm();
 const newScroll=document.querySelector?.(".catalog-candidates");if(newScroll){newScroll.scrollLeft=scrollLeft;newScroll.scrollTop=scrollTop;}
 document.querySelectorAll("[data-candidate-include]").forEach(input=>input.addEventListener("change",()=>{catalogUI.included=new Set(catalogIncluded());const folder=catalogUI.cards[Number(input.dataset.candidateInclude)].game.folder;if(input.checked)catalogUI.included.add(folder);else catalogUI.included.delete(folder);catalogUI.selectionDirty=true;catalogUI.primary="";catalogUI.patch={};catalogUI.preview=null;catalogDirty();catalogRenderReview();}));
 $("catalog-skip-group").addEventListener("click",()=>catalogQueueDecision("skip"));
 $("catalog-review-decisions").addEventListener("click",()=>{if(!catalogUI.dirty||window.confirm("Discard unsaved result edits?"))catalogRenderDecisions();});
 $("catalog-back").addEventListener("click",()=>{if(!catalogUI.dirty||window.confirm("Discard unpreviewed changes?"))catalogRenderPlan();});
 $("catalog-preview-group").addEventListener("click",()=>catalogPreviewGroup({confirm:pendingIdentity}));
 $("catalog-commit").addEventListener("click",()=>catalogPreviewGroup({confirm:pendingIdentity,queue:true}));
 $("catalog-keep-separate").addEventListener("click",()=>catalogQueueDecision("separate"));
 document.querySelectorAll("[data-candidate-primary]").forEach(button=>button.addEventListener("click",()=>{
  const card=catalogUI.cards[Number(button.dataset.candidatePrimary)];catalogUI.primary=card.game.folder;catalogUI.base=card.edit.fields;catalogUI.formProfile=card.profile;catalogDirty();catalogPreviewGroup();
 }));
 document.querySelectorAll("[data-candidate-value]").forEach(button=>button.addEventListener("click",()=>{
  const card=catalogUI.cards[Number(button.dataset.candidateValue)];catalogUI.patch[button.dataset.valueKey]=card.edit.fields[button.dataset.valueKey];catalogDirty();catalogRenderForm();
 }));
 document.querySelectorAll("[data-candidate-inspect]").forEach(button=>button.addEventListener("click",()=>{
  catalogInspectCandidate(button,Number(button.dataset.candidateInspect));
 }));
}
async function catalogInspectCandidate(button,index,offset=0){
 const card=catalogUI.cards[index],section=button.parentElement;
 button.disabled=true;
 try{
  if(offset===0)section.insertAdjacentHTML("beforeend",renderSourceSection(card)+renderMediaSection(card)+'<h4>Registered files & hashes</h4>');
  const data=await fetchJSON("/api/fingerprints?"+new URLSearchParams({platform:catalogUI.platform,game:card.game.folder,offset:String(offset),limit:"100"}));
  section.insertAdjacentHTML("beforeend",(data.fingerprints||[]).map(file=>'<details class="raw-details"><summary>'+cEscape(file.name||"Unnamed file")+'</summary><p>'+cEscape(file.release||file.variant||"Game-level file")+'</p><pre>'+cEscape(Object.entries(file).filter(([key])=>["crc32","md5","sha1","sha256","size","format"].includes(key)).map(([key,value])=>key+": "+value).join("\n"))+'</pre></details>').join("")+'<p class="secondary">'+Math.min(offset+100,data.total)+' of '+data.total+' registered files</p>');
  if(offset+100<data.total){
   const next=document.createElement("button");next.className="tab-button";next.textContent="Show more files";section.append(next);
   next.addEventListener("click",()=>catalogInspectCandidate(next,index,offset+100));
  }
 }catch(error){button.disabled=false;section.insertAdjacentHTML("beforeend",catalogNotice(error.message,"error"));}
}
async function catalogPreviewGroup({confirm=false,queue=false}={}){
 if(catalogUI.busy)return;catalogUI.busy=true;const version=catalogUI.version;
 try{
  const folders=[...catalogIncluded()],current=catalogUI.group.group,primary=catalogUI.primary||(folders.includes(current.Keep.Folder)?current.Keep.Folder:"");
  const response=await catalogPost("/api/catalog/fold-preview",{plan:catalogUI.plan.id,group:catalogUI.group.id,draft:{...(current.NativeDigest?{}:{folders}),primary,fields:catalogUI.patch,confirm:confirm||(!catalogUI.selectionDirty&&current.ReviewAccepted)||false,reason:current.ReviewReason||""}});
  catalogAcceptGroup(response.group);if(catalogUI.version!==version){catalogUI.dirty=true;return;}
  catalogUI.included=new Set([response.group.group.Keep,...(response.group.group.Merge||[])].map(c=>c.Folder));catalogUI.selectionDirty=false;catalogUI.preview=response.preview;catalogUI.dirty=false;catalogUI.primary="";
  const card=catalogUI.cards.find(c=>c.game.folder===response.group.group.Keep.Folder);
  catalogUI.base=response.group.result?.fields||card?.edit.fields||catalogUI.base;catalogUI.formProfile=response.preview?.profile||card?.profile||catalogUI.formProfile;
  catalogRenderReview();
  if(queue&&response.group.status==="ready"){catalogUI.busy=false;await catalogQueueDecision("merge");}
 }catch(error){catalogShowError(error);}finally{catalogUI.busy=false;}
}
async function catalogRefreshLibrary(){
 try{
  state.platforms=await fetchJSON("/api/platforms");
  $("platform-count").textContent=String(state.platforms.length);renderPlatforms();
 }catch(error){
  $("catalog-content").insertAdjacentHTML("beforeend",catalogNotice("Changes were saved, but the platform list could not refresh. Reload the page: "+error.message,"warn"));
 }
 if(state.platform===catalogUI.platform)selectPlatform(state.platform);
}
function catalogGroupKey(pg){return JSON.stringify((pg.candidates||[pg.group.Keep,...(pg.group.Merge||[])]).map(ref=>ref.Folder).sort());}
function catalogSessionSummary(){
 const result=catalogUI.completed;if(!result)return "";
 const remaining=catalogUI.plan?.groups.length;
 return catalogNotice(result.applied+" groups merged · "+result.kept_separate+" kept separate"+(remaining?" · "+remaining+" groups remaining":""))+
 (result.warnings||[]).map(w=>catalogNotice(w,"warn")).join("")+
 (result.backups.length?'<details class="raw-details"><summary>Recovery locations</summary><pre>'+cEscape(result.backups.join("\n"))+'</pre></details>':"");
}
function catalogRecordResult(response){
 const total=catalogUI.completed||(catalogUI.completed={applied:0,kept_separate:0,backups:[],warnings:[]});
 total.applied+=response.result.applied;total.kept_separate+=response.result.kept_separate;
 total.backups.push(...(response.result.backups||[]));if(response.warning)total.warnings.push(response.warning);
}
function catalogFinishReview(){
 const id=catalogUI.plan?.id;catalogUI.plan=null;catalogUI.group=null;catalogUI.mode="done";
 if(id)catalogPost("/api/catalog/fold-discard",{plan:id}).catch(()=>{});
 catalogHeader("Review complete",catalogUI.platform+" · No duplicate groups remain in this review.");
 $("catalog-content").innerHTML='<div class="catalog-empty"><div class="catalog-saved">✓</div><h3>All groups reviewed</h3><p>Your changes are saved.</p></div>'+catalogSessionSummary();
}
function catalogResumeButton(){
 $("catalog-content").insertAdjacentHTML("beforeend",'<button class="catalog-primary" id="catalog-resume">Continue review</button>');
 $("catalog-resume").addEventListener("click",()=>catalogBuildPlan(false,true));
}
async function catalogApply(decisions){
 if(catalogUI.busy||!decisions.length)return;
 const oldGroups=catalogUI.plan.groups||[];
 const excluded=new Set(oldGroups.filter(g=>g.status==="ready"&&!catalogUI.selected.has(g.id)).map(catalogGroupKey));
 catalogUI.busy=true;catalogUI.mode="applying";
 catalogHeader("Saving and preparing the next groups","Keep this window open. The review will continue automatically.");
 $("catalog-content").innerHTML='<div class="catalog-progress"><span class="catalog-spinner"></span><span>Saving selected groups and checking what remains…</span></div>';
 try{
  const response=await catalogPost("/api/catalog/fold-apply",{plan:catalogUI.plan.id,decisions,continue:true});
  catalogUI.plan=response.next_plan||null;catalogUI.decisions=new Map();catalogUI.cardsCache=new Map();catalogUI.dirty=false;catalogUI.group=null;catalogUI.artwork=new Map();
  catalogRecordResult(response);catalogUI.compare.clear();catalogUpdateTools();
  if(catalogUI.plan){
   catalogUI.selected=new Set(catalogUI.plan.groups.filter(g=>g.status==="ready"&&!excluded.has(catalogGroupKey(g))).map(g=>g.id));
   if(!catalogUI.plan.groups.length)catalogFinishReview();
   else catalogRenderPlan();
  }else{
   catalogUI.mode="done";catalogHeader("Changes saved",catalogUI.platform+" · The remaining groups need to be reloaded.");
   $("catalog-content").innerHTML=catalogSessionSummary()+catalogNotice(response.continuation_error||"Could not prepare the remaining groups.","warn");
   catalogResumeButton();
  }
  await catalogRefreshLibrary();
 }catch(error){
  if(error.consumed)catalogUI.plan=null;
  catalogUI.mode="done";catalogHeader("Review paused","Reload the remaining groups to continue safely.");
  $("catalog-content").innerHTML=catalogNotice(error.message,"error");
  if(error.result?.applied||error.result?.kept_separate){catalogRecordResult(error);await catalogRefreshLibrary();}
  $("catalog-content").insertAdjacentHTML("beforeend",catalogSessionSummary());catalogResumeButton();
 }finally{catalogUI.busy=false;}
}

function catalogBatchDecisions(){
 const decisions=new Map(catalogUI.decisions||[]);
 for(const id of catalogUI.selected){
  const pg=catalogUI.plan?.groups.find(g=>g.id===id);
  if(pg?.status==="ready"&&!["separate","skip"].includes(decisions.get(id)?.action))decisions.set(id,{id,revision:pg.revision,action:"merge"});
 }
 return [...decisions.values()].filter(d=>d.action!=="skip");
}
function catalogQueueNoticeContent(){
 const decisions=catalogBatchDecisions(),merged=decisions.filter(d=>d.action==="merge").length,separate=decisions.length-merged;
 return merged+' to merge · '+separate+' to keep separate <span>Nothing saved yet. Apply all decisions at the end.</span>';
}
function catalogQueueNotice(){return catalogUI.plan?'<div class="catalog-queue-status" id="catalog-queue-status" role="status">'+catalogQueueNoticeContent()+'</div>':"";}
async function catalogQueueDecision(action){
 const pg=catalogUI.group;if(!pg)return;
 if(!catalogUI.decisions)catalogUI.decisions=new Map();
 catalogUI.decisions.set(pg.id,{id:pg.id,revision:pg.revision,action});
 if(action==="merge")catalogUI.selected.add(pg.id);else catalogUI.selected.delete(pg.id);
 catalogUI.dirty=false;catalogUI.busy=false;
 const next=catalogNextGroup();
 if(next)await catalogReviewGroup(next.id);else catalogRenderDecisions();
}
function catalogRenderDecisions(){
 catalogUI.mode="decisions";catalogUI.group=null;catalogUI.dirty=false;
 const decisions=catalogBatchDecisions(),merge=decisions.filter(d=>d.action==="merge").length,separate=decisions.length-merge,remaining=catalogUI.plan.groups.length-decisions.length;
 catalogHeader("Review your decisions","One batch will save these changes. You can still go back and change any decision.");
 $("catalog-content").innerHTML=catalogSessionSummary()+'<div id="catalog-feedback"></div>'+catalogQueueNotice()+
 '<div class="catalog-decision-totals"><strong>'+merge+' merge</strong><strong>'+separate+' keep separate</strong><span>'+remaining+' unresolved / skipped</span></div>'+
 '<div class="catalog-decision-list">'+decisions.map(d=>{const pg=catalogUI.plan.groups.find(g=>g.id===d.id),g=pg.group;return '<button class="catalog-decision-row" data-plan-review="'+d.id+'"><span>'+cEscape(g.Keep.Title||g.Keep.Folder)+'</span><span>'+ (d.action==="merge"?(1+(g.Merge||[]).length)+' cards → 1':"Keep separate")+'</span><span>Review →</span></button>';}).join("")+'</div>'+
 (remaining?catalogNotice(remaining+" groups have no merge or separation decision. They will remain unchanged.","warn"):"")+
 '<div class="catalog-actions"><button class="tab-button" id="catalog-back-plan">Back to review</button><button class="catalog-primary" id="catalog-save-decisions" '+(!decisions.length?"disabled":"")+'>Apply '+decisions.length+' decisions</button></div>';
 $("catalog-back-plan").addEventListener("click",catalogRenderPlan);
 $("catalog-save-decisions").addEventListener("click",()=>catalogApply(catalogBatchDecisions()));
 document.querySelectorAll("[data-plan-review]").forEach(button=>button.addEventListener("click",()=>catalogReviewGroup(Number(button.dataset.planReview))));
}
$("catalog-dialog").addEventListener("cancel",event=>{event.preventDefault();catalogClose();});
$("catalog-close").addEventListener("click",catalogClose);
$("catalog-edit-card").addEventListener("click",catalogEditCard);
$("catalog-build-plan").addEventListener("click",()=>catalogBuildPlan());
$("catalog-compare").addEventListener("click",()=>catalogBuildPlan(true));
$("catalog-add-compare").addEventListener("click",()=>{
 if(!state.selectedFolder)return;if(catalogUI.comparePlatform!==state.platform){catalogUI.compare.clear();catalogUI.comparePlatform=state.platform;}
 if(catalogUI.compare.has(state.selectedFolder))catalogUI.compare.delete(state.selectedFolder);else catalogUI.compare.add(state.selectedFolder);
 $("catalog-add-compare").textContent=catalogUI.compare.has(state.selectedFolder)?"Remove from comparison":"Add to comparison";catalogUpdateTools();
});
const catalogObserver=new MutationObserver(catalogUpdateTools);
catalogObserver.observe($("detail-title"),{childList:true});catalogObserver.observe($("export-bar"),{attributes:true,attributeFilter:["class"]});catalogObserver.observe($("catalog-tools").parentElement,{attributes:true,attributeFilter:["class"]});

// Editing is a mode: all inputs for the selected record appear together.
// A card revision covers every target; one Save commits all its drafts atomically.
const catalogInline={mode:false,dirty:false,saving:false,loading:false,version:0,card:null,platform:"",folder:"",target:{kind:"library",match:-1},edits:new Map(),invalid:new Map(),extra:new Set(),preferredSource:"",message:"",error:"",nameMode:"",nameDraft:""};
const catalogRecordLabels={title:"Title",release_date:"Release date",developer:"Developer",publisher:"Publisher",genres:"Genres",players:"Players",max_players:"Maximum players",themes:"Themes",collections:"Collections",franchises:"Franchises",rating:"Rating",aliases:"Library aliases",alternate_titles:"Alternate titles",description:"Shared description",descriptions:"Descriptions"};
const catalogRecordLists=new Set(["aliases","alternate_titles","genres","themes","collections","franchises","platforms"]);
function catalogInlineAvailable(){return catalogUI.enabled&&!state.exportMode&&state.view==="browse";}
function catalogInlineCurrent(){return catalogInline.platform===state.platform&&catalogInline.folder===state.selectedFolder;}
function catalogEditorActive(){return catalogInlineAvailable()&&catalogInline.mode&&catalogInlineCurrent();}
function catalogTargetKey(target){return JSON.stringify(target);}
function catalogTargetBase(target=catalogInline.target){
 if(!target||target.kind==="library")return catalogInline.card?.fields||{};
 return catalogInline.card?.records?.find(r=>catalogTargetKey(r.target)===catalogTargetKey(target))?.fields||{};
}
function catalogTargetValues(){const f=catalogInline;return {...catalogTargetBase(),...(f.edits.get(catalogTargetKey(f.target))?.fields||{})};}
function catalogInlineFields(){return catalogInlineCurrent()&&catalogInline.card?catalogInline.card.fields:(state.game?.catalog_fields||{});}
function catalogInlineOrigin(key){
 const fields=catalogInlineFields(),profile=state.game?.profile||{};
 if(Object.hasOwn(fields,key)||(key==="description"&&profile.source==="local"))return key==="title"?"Library title":"Saved in library";
 if(key==="description")return profile.source?sourceLabel(profile.source)+(profile.description_language?" · "+profile.description_language.toUpperCase():""):"From sources";
 return "From sources";
}
function catalogInlineField(key,label,value,body=""){
 if(!catalogInlineAvailable())return body||fact(label,value);
 if(!value&&!body)return "";
 return '<div class="fact-row catalog-inline-field"><div class="catalog-inline-label"><span class="fact-label">'+cEscape(label)+'</span><button type="button" class="catalog-pencil" data-inline-edit="'+key+'" aria-label="Edit '+escapeAttr(label)+'">✎</button></div>'+(body||'<div class="fact-value">'+cEscape(value)+'</div>')+'<small class="catalog-value-origin">'+cEscape(catalogInlineOrigin(key))+'</small></div>';
}
function catalogInlineTitle(value){
 if(!catalogInlineAvailable())return cEscape(value);
 return cEscape(value)+(catalogInline.mode?"":' <button type="button" class="catalog-pencil" data-inline-edit="title" aria-label="Edit library title">✎</button>');
}
function catalogEditorTargets(){
 const f=catalogInline,result=[{target:{kind:"library",match:-1},label:"Library fields · explicit overrides"}],sources=f.card?.registry?.sources||[];
 for(const source of sources){
  const records=(f.card.records||[]).filter(r=>r.target.source===source.id&&r.target.match>=0);
  for(const r of records)result.push({target:r.target,label:source.name+" · "+(r.fields.release||(r.target.release_index==null?"whole game":r.label))});
  const scopes=["",...(f.card.releases||[])].filter(scope=>!records.some(r=>r.target.release_index==null&&(r.fields.release||"")===scope));
  if(scopes.length)result.push({target:{kind:"manual",source:source.id,match:-1,scope:scopes[0]},label:source.name+(records.length?" · new release record":" · whole game")});
 }
 for(const r of f.card?.records||[]){
  if(sources.some(s=>s.id===r.target.source)&&(r.target.match>=0||!Object.keys(r.fields).length))continue;
  result.push({target:r.target,label:sourceLabel(r.target.source)+" · "+r.label});
 }
 // Keep unsaved scoped records selectable when moving between targets.
 for(const draft of f.edits.values())if(!result.some(r=>catalogTargetKey(r.target)===catalogTargetKey(draft.target)))result.push({target:draft.target,label:sourceLabel(draft.target.source)+" · "+(draft.target.scope||"whole game")+" · draft"});
 return result;
}
function catalogInlineBanner(){
 const f=catalogInline;if(!catalogEditorActive())return "";
 if(f.loading)return '<div class="catalog-inline-banner" role="status">Opening editable fields…</div>';
 if(!f.card)return '<div class="catalog-inline-banner"><span role="alert">'+cEscape(f.error)+'</span><button class="tab-button" id="catalog-editor-reload">Retry</button></div>';
 const key=catalogTargetKey(f.target),targets=catalogEditorTargets(),source=f.card.registry?.sources?.find(s=>s.id===f.target.source),disabled=f.saving?' disabled':'';
 const count=f.edits.size,hasOverrides=Object.keys(f.card.fields||{}).some(k=>!["title","aliases","alternate_titles"].includes(k));
 const help=f.target.kind==="library"?"Library values override selected sources. Clear a value to resume normal source selection. Exported names still follow release/title settings.":source?"Only fields you change belong to this variant. Enable and order it in Export → Advanced → Metadata, or Settings → Source priorities.": "Editing this exact saved source record. Refreshing that provider can replace these changes; use a named variant to retain your own version.";
 return '<div class="catalog-inline-banner catalog-editor-bar"><div class="catalog-editor-heading"><strong>Edit card</strong><span id="catalog-editor-count">'+count+' changed '+(count===1?"record":"records")+'</span></div>'+
 '<label>Editing<select id="catalog-editor-target"'+disabled+'>'+targets.map(r=>'<option value="'+escapeAttr(catalogTargetKey(r.target))+'" '+(catalogTargetKey(r.target)===key?'selected':'')+'>'+cEscape(r.label)+(f.edits.has(catalogTargetKey(r.target))?' · edited':'')+'</option>').join("")+'</select></label>'+
 (f.target.kind==="manual"?'<label>Applies to<select id="catalog-editor-scope"'+disabled+'><option value="">Whole game</option>'+(f.card.releases||[]).map(r=>'<option value="'+escapeAttr(r)+'" '+(f.target.scope===r?'selected':'')+'>'+cEscape(r)+'</option>').join("")+'</select></label>':"")+
 '<div class="catalog-inline-actions"><button class="tab-button" id="catalog-new-source"'+disabled+'>New named variant</button>'+(source?'<button class="tab-button" id="catalog-rename-source"'+disabled+'>Rename variant</button>':"")+'</div>'+
 (f.nameMode?'<div class="catalog-source-name"><label>Variant name<input id="catalog-source-name" maxlength="120" value="'+escapeAttr(f.nameDraft)+'"'+disabled+'></label><button class="tab-button" id="catalog-save-source-name"'+disabled+'>'+(f.nameMode==="rename"?"Save name":"Create variant")+'</button><button class="tab-button" id="catalog-cancel-source-name"'+disabled+'>Cancel</button></div>':"")+
 '<p>'+cEscape(help)+'</p>'+(hasOverrides&&f.target.kind!=="library"?'<p class="catalog-editor-hint">This card also has explicit library values. They still take priority; choose Library fields to review or clear them.</p>':"")+
 '<div class="catalog-inline-actions"><button class="catalog-primary" id="catalog-inline-save" '+(!f.dirty||f.saving||f.invalid.size?'disabled':'')+'>'+(f.saving?'Saving…':'Save changes')+'</button><button class="tab-button" id="catalog-editor-discard" '+((!f.dirty&&!f.invalid.size)||f.saving?'disabled':'')+'>Discard changes</button><button class="tab-button" id="catalog-inline-cancel"'+disabled+'>Finish editing</button><button class="tab-button" id="catalog-inline-export"'+disabled+'>Preview export</button></div>'+
 '<span id="catalog-editor-status" role="status">'+cEscape(f.error||f.message||(f.dirty?"Unsaved changes":""))+'</span></div>';
}
function catalogEditorControl(key,value,label= catalogRecordLabels[key]||key){
 const f=catalogInline,disabled=f.saving?' disabled':'',base=catalogTargetBase(),exists=Object.hasOwn(base,key)||Object.hasOwn(catalogTargetValues(),key);
 const attr=' data-card-field="'+escapeAttr(key)+'" aria-label="'+escapeAttr(label)+'"';
 let control,kind="text";
 if(typeof value==="boolean"){kind="boolean";control='<select'+attr+disabled+'><option value="true" '+(value?'selected':'')+'>True</option><option value="false" '+(!value?'selected':'')+'>False</option></select>';}
 else if(typeof value==="number"){kind="number";control='<input type="number" step="any"'+attr+' value="'+value+'"'+disabled+'>';}
 else if((catalogRecordLists.has(key)&&(value==null||Array.isArray(value)))||(Array.isArray(value)&&value.every(v=>typeof v==="string"))){kind="list";control='<textarea rows="3"'+attr+disabled+'>'+cEscape((value||[]).join("\n"))+'</textarea>';}
 else if((value&&typeof value==="object")||f.invalid.has(catalogTargetKey(f.target)+"|"+key)){kind="json";control='<textarea rows="5"'+attr+' spellcheck="false"'+disabled+'>'+cEscape(f.invalid.get(catalogTargetKey(f.target)+"|"+key)??JSON.stringify(value,null,2))+'</textarea>';}
 else {const text=value??"";control=key==="description"||String(text).includes("\n")?'<textarea rows="5"'+attr+disabled+'>'+cEscape(text)+'</textarea>':'<input'+attr+' value="'+escapeAttr(text)+'"'+disabled+'>';}
 return '<div class="catalog-record-field" data-field-kind="'+kind+'"><label><span>'+cEscape(label)+'</span>'+control+'</label>'+(kind==="json"?'<small>Structured value · JSON</small>':kind==="list"?'<small>One value per line</small>':"")+(exists&&!(f.target.kind==="library"&&key==="title")?'<button type="button" class="catalog-reset" data-card-remove="'+escapeAttr(key)+'"'+disabled+'>Remove field</button>':"")+'</div>';
}
function catalogRenderEditor(response,coverHTML){
 const f=catalogInline;if(!f.card)return '<section class="game-overview">'+(f.loading?"":"<p>Reload this card to edit its fields.</p>")+'</section>';
 const fields=catalogTargetValues(),library=f.target.kind==="library",details=f.target.kind==="source"&&f.target.match===-1;
 const common=details?[]:["title","release_date","developer","publisher","genres",(!library&&Object.hasOwn(fields,"max_players")&&!Object.hasOwn(fields,"players")?"max_players":"players"),...(library?["aliases","alternate_titles"]:[])];
 const metadata=common.map(k=>catalogEditorControl(k,fields[k],k==="title"&&library?"Library title":catalogRecordLabels[k])).join("");
 const descriptions=fields.descriptions||{},langs=Array.from(new Set([...Object.keys(descriptions),"en",...f.extra])).sort();
 const prose=details?"":'<div class="catalog-description-fields"><h3>Descriptions</h3>'+langs.map(lang=>'<label>'+cEscape(catalogLanguages[lang]||lang)+'<textarea rows="5" data-card-description="'+escapeAttr(lang)+'" aria-label="Description '+escapeAttr(lang)+'" '+(f.saving?'disabled':'')+'>'+cEscape(descriptions[lang]||"")+'</textarea></label>').join("")+'<div class="catalog-inline-actions"><input id="catalog-description-language" aria-label="Add description language" placeholder="Language code, e.g. ru"><button class="tab-button" id="catalog-add-language">Add language</button></div>'+(library?catalogEditorControl("description",fields.description,"Shared description · all languages"):"")+'</div>';
 const omitted=new Set([...common,"descriptions",...(library?["description"]:[])]);
 const extra=Object.keys(fields).filter(k=>!omitted.has(k)).map(k=>catalogEditorControl(k,fields[k])).join("");
 return '<section class="game-overview catalog-record-editor"><div class="catalog-editor-layout"><div class="cover-panel">'+coverHTML+'</div><div><div class="catalog-record-grid">'+metadata+'</div>'+prose+
 (extra?'<details class="catalog-record-extra" '+(details?'open':'')+'><summary>Other source fields ('+Object.keys(fields).filter(k=>!omitted.has(k)).length+')</summary><div class="catalog-record-grid">'+extra+'</div></details>':"")+
 (!library?'<div class="catalog-inline-actions"><input id="catalog-extra-field" aria-label="New source field" placeholder="Additional field name"><button class="tab-button" id="catalog-add-field">Add field</button></div>':"")+
 '<p class="secondary">Empty or removed fields fall through to other selected sources. Record titles and identifiers remain source evidence; exported names follow the selected release and title settings.</p></div></div></section>';
}
function catalogEqualValue(a,b){
 const canonical=v=>Array.isArray(v)?v.map(canonical):v&&typeof v==="object"?Object.fromEntries(Object.keys(v).sort().map(k=>[k,canonical(v[k])])):v;
 return JSON.stringify(canonical(a))===JSON.stringify(canonical(b));
}
function catalogEditorChange(key,value){
 const f=catalogInline,token=catalogTargetKey(f.target),draft=f.edits.get(token)||{target:{...f.target},fields:Object.create(null)},base=catalogTargetBase();
 if(catalogEqualValue(value,base[key])||(value==null&&!Object.hasOwn(base,key)))delete draft.fields[key];else draft.fields[key]=value;
 if(Object.keys(draft.fields).length)f.edits.set(token,draft);else f.edits.delete(token);
 f.dirty=f.edits.size>0;f.error="";f.message="";catalogEditorActions();
}
function catalogEditorDescription(lang,value){
 const descriptions={...(catalogTargetValues().descriptions||{})};
 if(value.trim())descriptions[lang]=value;else delete descriptions[lang];
 catalogEditorChange("descriptions",Object.keys(descriptions).length?descriptions:null);
}
function catalogEditorActions(){
 const f=catalogInline;
 if($("catalog-inline-save"))$("catalog-inline-save").disabled=!f.dirty||f.saving||f.invalid.size>0;
 if($("catalog-editor-discard"))$("catalog-editor-discard").disabled=(!f.dirty&&!f.invalid.size)||f.saving;
 if($("catalog-editor-count"))$("catalog-editor-count").textContent=f.edits.size+" changed "+(f.edits.size===1?"record":"records");
 if($("catalog-editor-status"))$("catalog-editor-status").textContent=f.error||f.message||(f.invalid.size?"Check structured field values":f.dirty?"Unsaved changes":"");
}
function catalogInlineRender(){const scroll=$("detail-body").scrollTop;renderDetails();$("detail-body").scrollTop=scroll;catalogUpdateTools();}
function catalogLeaveInline(){
 const f=catalogInline;if(f.saving)return false;
 if((f.dirty||f.invalid.size)&&!window.confirm("Discard unsaved card changes?"))return false;
 f.version++;f.mode=false;f.loading=false;f.dirty=false;f.edits=new Map();f.invalid=new Map();f.extra=new Set();f.card=null;f.error="";f.message="";f.nameMode="";return true;
}
async function catalogEditCard(){
 if(!catalogInlineAvailable()||catalogUI.busy)return;
 if(catalogInline.mode){if(catalogLeaveInline())catalogInlineRender();return;}
 await catalogStartInline();
}
async function catalogStartInline(key){
 const f=catalogInline;if(!catalogInlineAvailable()||catalogUI.busy)return;
 if(f.mode&&f.card&&catalogInlineCurrent()){if(key)document.querySelectorAll("[data-card-field]").forEach(e=>{if(e.dataset.cardField===key)e.focus?.();});return;}
 const platform=state.platform,folder=state.selectedFolder,version=++f.version;
 Object.assign(f,{mode:true,loading:true,platform,folder,error:"",message:""});state.detailTab="preview";catalogInlineRender();
 try{
  const card=await fetchJSON("/api/catalog/edit?"+new URLSearchParams({platform,game:folder}));
  if(version!==f.version||platform!==state.platform||folder!==state.selectedFolder)return;
  if(card.editor_error)throw new Error(card.editor_error);
  f.card=card;f.target={kind:"library",match:-1};f.edits=new Map();f.invalid=new Map();f.extra=new Set();
  if(f.preferredSource&&card.registry?.sources?.some(s=>s.id===f.preferredSource)){
   f.target=card.records?.find(r=>r.target.source===f.preferredSource&&r.target.match>=0&&r.target.release_index==null)?.target||{kind:"manual",source:f.preferredSource,match:-1,scope:""};
  }
  if(state.sourceDefaults)state.sourceDefaults.source_labels={...(state.sourceDefaults.source_labels||{}),...Object.fromEntries((card.registry?.sources||[]).map(s=>[s.id,s.name]))};
 }catch(error){if(version===f.version)f.error=error.message;}
 finally{if(version===f.version){f.loading=false;catalogInlineRender();if(key)document.querySelectorAll("[data-card-field]").forEach(e=>{if(e.dataset.cardField===key)e.focus?.();});}}
}
async function catalogSaveInline(){
 const f=catalogInline;if(f.saving||!f.dirty||f.invalid.size||!catalogInlineCurrent())return;
 const platform=f.platform,folder=f.folder,target=f.target;f.saving=true;catalogUI.busy=true;f.error="";catalogInlineRender();
 try{
  const response=await catalogPost("/api/catalog/edit",{platform,game:folder,revision:f.card.revision,edits:Array.from(f.edits.values())});
  f.card=response.card;f.edits=new Map();f.invalid=new Map();f.dirty=false;f.message=response.warning||"Saved to your library.";
  if(target.kind==="manual")f.target=f.card.records.filter(r=>r.target.source===target.source&&r.target.match>=0&&r.target.release_index==null).find(r=>(r.fields.release||"")===(target.scope||""))?.target||{kind:"library",match:-1};
  if(state.platform===platform&&state.selectedFolder===folder){
   state.game.profile=response.card.profile;state.game.catalog_fields=response.card.fields;state.game.catalog_yaml=response.card.yaml;
   if(state.game.game){state.game.game.title=response.card.fields.title;state.game.game.aliases=response.card.fields.aliases||[];}
   const row=state.games.find(g=>g.folder===folder);if(row)row.title=response.card.fields.title;
   renderGames();if(typeof resetPreviews==="function")resetPreviews();
   try{
    const fresh=await fetchJSON("/api/game?"+new URLSearchParams({platform,game:folder}));
    if(fresh?.game)state.game=fresh;
    state.sourceDefaults=await fetchJSON("/api/export-options");
    if(typeof renderSourceControls==="function")renderSourceControls();
   }catch(error){f.message+=" Could not refresh: "+error.message;}
  }
  catalogUI.cardsCache=new Map();
 }catch(error){f.error=error.message;}
 finally{f.saving=false;catalogUI.busy=false;catalogInlineRender();}
}
async function catalogSaveSourceName(){
 const f=catalogInline;if(f.saving)return;
 const name=$("catalog-source-name").value,id=f.nameMode==="rename"?f.target.source:"";
 f.nameDraft=name;f.saving=true;catalogUI.busy=true;f.error="";catalogInlineRender();
 try{
  const response=await catalogPost("/api/catalog/curation",{revision:f.card.registry.revision,id,name});
  f.card.registry=response.registry;f.nameMode="";f.preferredSource=response.source.id;
  if(!id){f.target={kind:"manual",source:response.source.id,match:-1,scope:""};f.extra=new Set();}
  f.message=id?"Variant renamed. Saved profiles keep the same source.":"Variant created. Add its fields, then Save changes; enable it in your metadata priorities.";
  if(state.sourceDefaults)state.sourceDefaults.source_labels={...(state.sourceDefaults.source_labels||{}),[response.source.id]:response.source.name};
  try{state.sourceDefaults=await fetchJSON("/api/export-options");if(typeof renderSourceControls==="function")renderSourceControls();}catch(error){f.message+=" Could not refresh source choices: "+error.message;}
  if(typeof librarySettingsUI!=="undefined")librarySettingsUI.sections=null;
 }catch(error){f.error=error.message;}
 finally{f.saving=false;catalogUI.busy=false;catalogInlineRender();}
}
function catalogBindInline(){
 document.querySelectorAll("[data-inline-edit]").forEach(e=>e.addEventListener("click",()=>catalogStartInline(e.dataset.inlineEdit)));
 if(!catalogEditorActive())return;
 const f=catalogInline;
 $("catalog-editor-reload")?.addEventListener("click",()=>{f.mode=false;catalogStartInline();});
 $("catalog-editor-target")?.addEventListener("change",e=>{f.target=JSON.parse(e.target.value);f.extra=new Set();f.error="";f.message="";f.nameMode="";if(f.card.registry.sources.some(s=>s.id===f.target.source))f.preferredSource=f.target.source;catalogInlineRender();});
 $("catalog-editor-scope")?.addEventListener("change",e=>{f.target={...f.target,scope:e.target.value};f.extra=new Set();catalogInlineRender();});
 $("catalog-new-source")?.addEventListener("click",()=>{f.nameMode="create";f.nameDraft="";catalogInlineRender();$("catalog-source-name")?.focus?.();});
 $("catalog-rename-source")?.addEventListener("click",()=>{f.nameMode="rename";f.nameDraft=f.card.registry.sources.find(s=>s.id===f.target.source)?.name||"";catalogInlineRender();$("catalog-source-name")?.focus?.();});
 $("catalog-source-name")?.addEventListener("input",e=>{f.nameDraft=e.target.value;});
 $("catalog-save-source-name")?.addEventListener("click",catalogSaveSourceName);
 $("catalog-cancel-source-name")?.addEventListener("click",()=>{f.nameMode="";catalogInlineRender();});
 $("catalog-inline-save")?.addEventListener("click",catalogSaveInline);
 $("catalog-inline-cancel")?.addEventListener("click",()=>{if(catalogLeaveInline())catalogInlineRender();});
 $("catalog-editor-discard")?.addEventListener("click",()=>{f.edits=new Map();f.invalid=new Map();f.dirty=false;f.error="";f.message="";catalogInlineRender();});
 $("catalog-inline-export")?.addEventListener("click",()=>{if(catalogLeaveInline())setExportMode(true);});
 document.querySelectorAll("[data-card-field]").forEach(input=>input.addEventListener("input",()=>{
  if(f.saving)return;
  const key=input.dataset.cardField,kind=input.closest("[data-field-kind]").dataset.fieldKind,token=catalogTargetKey(f.target)+"|"+key;
  try{
   const value=kind==="json"?(input.value.trim()?JSON.parse(input.value):null):kind==="list"?input.value.split("\n").map(v=>v.trim()).filter(Boolean):kind==="number"?(input.value===""?null:Number(input.value)):kind==="boolean"?input.value==="true":input.value||null;
   if(kind==="number"&&value!==null&&!Number.isFinite(value))throw new Error("Invalid number");
   f.invalid.delete(token);catalogEditorChange(key,value);
  }catch(error){f.invalid.set(token,input.value);f.error="Check "+key+": "+error.message;catalogEditorActions();}
 }));
 document.querySelectorAll("[data-card-description]").forEach(input=>input.addEventListener("input",()=>{if(!f.saving)catalogEditorDescription(input.dataset.cardDescription,input.value);}));
 document.querySelectorAll("[data-card-remove]").forEach(button=>button.addEventListener("click",()=>{if(f.saving)return;f.invalid.delete(catalogTargetKey(f.target)+"|"+button.dataset.cardRemove);catalogEditorChange(button.dataset.cardRemove,null);catalogInlineRender();}));
 $("catalog-add-language")?.addEventListener("click",()=>{const lang=$("catalog-description-language").value.trim().toLowerCase();if(!/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/.test(lang)){f.error="Use a language code such as en, ru or pt-BR.";catalogEditorActions();return;}f.extra.add(lang);catalogInlineRender();});
 $("catalog-add-field")?.addEventListener("click",()=>{const key=$("catalog-extra-field").value.trim();if(!key||["matches","__proto__","constructor","prototype"].includes(key)||Object.hasOwn(catalogTargetValues(),key))return;catalogEditorChange(key,"");catalogInlineRender();});
}
window.addEventListener?.("beforeunload",event=>{if(catalogInline.dirty||catalogInline.invalid.size||catalogInline.saving){event.preventDefault();event.returnValue="";}});

catalogCapabilities();
