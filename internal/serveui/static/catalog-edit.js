
/* One page-session workspace: plans are prepared by Go, never inferred in JS. */
const catalogUI = {enabled:false, plan:null, selected:new Set(), compare:new Set(), controller:null, busy:false, dirty:false, mode:"", cards:[], group:null, patch:{}, base:{}, revision:"", platform:"", folder:""};
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
function catalogHeader(title,subtitle){$("catalog-title").textContent=title;$("catalog-subtitle").textContent=subtitle||"";}
function catalogOpen(title,subtitle){
 catalogUI.version=(catalogUI.version||0)+1;catalogHeader(title,subtitle);const dialog=$("catalog-dialog");if(!dialog.open)dialog.showModal();$("catalog-content").innerHTML='<div class="catalog-progress"><span class="catalog-spinner"></span><span id="catalog-progress-text">Preparing your workspace…</span></div>';
}
function catalogShowError(error){if(error.name==="AbortError")return;const slot=$("catalog-feedback");if(slot)slot.innerHTML=catalogNotice(error.message,"error");else $("catalog-content").innerHTML=catalogNotice(error.message,"error");}
async function catalogClose(){
 if(catalogUI.busy&&(catalogUI.mode==="applying"||catalogUI.mode==="saving"))return;
 if(catalogUI.dirty&&!window.confirm("Discard unsaved changes?"))return;
 catalogUI.version=(catalogUI.version||0)+1;catalogUI.controller?.abort();catalogUI.dirty=false;catalogUI.busy=false;
 if(catalogUI.plan){const id=catalogUI.plan.id;catalogUI.plan=null;catalogPost("/api/catalog/fold-discard",{plan:id}).catch(()=>{});}
 $("catalog-dialog").close();
}
function catalogUpdateTools(){
 if(catalogUI.comparePlatform&&catalogUI.comparePlatform!==state.platform){catalogUI.compare.clear();catalogUI.comparePlatform=state.platform;}
 $("catalog-add-compare").textContent=catalogUI.compare.has(state.selectedFolder)?"Remove from comparison":"Add to comparison";
 $("catalog-tools").classList.toggle("hidden",!catalogUI.enabled||state.view!=="browse"||state.exportMode);
 $("catalog-edit-card").disabled=!state.game;$("catalog-build-plan").disabled=!state.platform;
 $("catalog-add-compare").disabled=!state.game;
 $("catalog-compare").disabled=catalogUI.compare.size<2;
 $("catalog-compare").textContent="Compare ("+catalogUI.compare.size+")";
}
async function catalogCapabilities(){
 try{const response=await fetchJSON("/api/catalog");catalogUI.enabled=!!response.editing;catalogUpdateTools();}catch{/* Read-only older servers remain browsable. */}
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
function catalogDirty(){catalogUI.version=(catalogUI.version||0)+1;catalogUI.approvedPatch=null;catalogUI.dirty=true;$("catalog-commit")?.setAttribute("disabled","");const feedback=$("catalog-preview-feedback");if(feedback)feedback.textContent="Changes need a new preview.";}
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
async function catalogEditCard(){
 if(!state.game||catalogUI.busy)return;
 catalogUI.mode="edit";catalogUI.platform=state.platform;catalogUI.folder=state.selectedFolder;catalogUI.patch={};catalogUI.dirty=false;catalogUI.approvedPatch=null;
 catalogOpen("Edit game","Your corrections take priority over source metadata.");const version=catalogUI.version;
 try{
  const card=await fetchJSON("/api/catalog/edit?"+new URLSearchParams({platform:catalogUI.platform,game:catalogUI.folder}));
  if(catalogUI.version!==version)return;catalogUI.base=card.fields;catalogUI.revision=card.revision;catalogUI.formProfile=card.profile;catalogUI.group=null;
  $("catalog-content").innerHTML='<div id="catalog-feedback"></div><div class="catalog-edit-grid"><section><div id="catalog-form-host"></div></section><section><div class="catalog-preview" id="catalog-result-preview">'+catalogProfilePreview(card)+'</div><p class="secondary" id="catalog-preview-feedback">Edit values and preview before saving.</p><div class="catalog-actions"><button class="tab-button" id="catalog-preview-edit">Preview changes</button><button class="catalog-primary" id="catalog-commit" disabled>Save card</button></div></section></div>';
  catalogRenderForm();$("catalog-preview-edit").addEventListener("click",catalogPreviewEdit);$("catalog-commit").addEventListener("click",catalogSaveEdit);
 }catch(error){catalogShowError(error);}
}
async function catalogPreviewEdit(){
 const version=catalogUI.version,patch=JSON.parse(JSON.stringify(catalogUI.patch));
 try{
  const card=await catalogPost("/api/catalog/edit-preview",{platform:catalogUI.platform,game:catalogUI.folder,revision:catalogUI.revision,fields:patch});
  if(catalogUI.version!==version)return;catalogUI.approvedPatch=patch;
  $("catalog-result-preview").innerHTML=catalogProfilePreview(card);$("catalog-preview-feedback").textContent="This is the card that will be saved.";$("catalog-commit").disabled=false;$("catalog-feedback").innerHTML="";
 }catch(error){catalogShowError(error);}
}
async function catalogSaveEdit(){
 if(catalogUI.busy||!catalogUI.approvedPatch)return;catalogUI.busy=true;catalogUI.mode="saving";$("catalog-commit").disabled=true;
 try{
  const response=await catalogPost("/api/catalog/edit",{platform:catalogUI.platform,game:catalogUI.folder,revision:catalogUI.revision,fields:catalogUI.approvedPatch});
  catalogUI.dirty=false;catalogUI.revision=response.card.revision;catalogUI.base=response.card.fields;catalogUI.patch={};
  $("catalog-feedback").innerHTML=catalogNotice(response.warning||"Saved. Your corrections stay with this card.",response.warning?"warn":"");
  $("catalog-preview-feedback").textContent="Saved to the catalog.";
  if(state.platform===catalogUI.platform){
   const row=state.games.find(g=>g.folder===catalogUI.folder);if(row)row.title=response.card.fields.title;renderGames();
   if(state.selectedFolder===catalogUI.folder)await selectGame(catalogUI.folder);
  }
 }catch(error){catalogShowError(error);}finally{catalogUI.busy=false;}
}
async function catalogReadPlan(response){
 if(!response.ok){const data=await response.json();throw new Error(data.error||"Could not build a plan");}
 const reader=response.body.getReader(),decoder=new TextDecoder();let buffer="",result;
 const frame=line=>{if(!line.trim())return;const value=JSON.parse(line);if(value.error)throw new Error(value.error);if(value.result)result=value.result;if(value.progress){const p=value.progress;const el=$("catalog-progress-text");if(el)el.textContent=(p.Stage||"Preparing")+(p.Total?" · "+p.Current+" / "+p.Total:"")+(p.Item?" · "+p.Item:"");}};
 try{for(;;){const {value,done}=await reader.read();if(done)break;buffer+=decoder.decode(value,{stream:true});let end;while((end=buffer.indexOf("\n"))>=0){frame(buffer.slice(0,end));buffer=buffer.slice(end+1);}}buffer+=decoder.decode();frame(buffer);if(!result)throw new Error("Planning stopped before completion. Build a new plan.");return result;}finally{reader.releaseLock?.();}
}
async function catalogBuildPlan(manual=false){
 if(!state.platform||catalogUI.busy)return;
 if(catalogUI.plan)await catalogPost("/api/catalog/fold-discard",{plan:catalogUI.plan.id}).catch(()=>{});
 catalogUI.mode="planning";catalogUI.busy=true;catalogUI.dirty=false;catalogUI.controller=new AbortController();catalogUI.platform=state.platform;
 catalogOpen(manual?"Compare selected games":"Find possible duplicates","Building a plan for "+state.platform+". Your catalog stays unchanged.");const version=catalogUI.version;
 try{
  const response=await fetch("/api/catalog/fold-plan?stream=1",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({platform:state.platform,games:manual?[...catalogUI.compare]:[]}),signal:catalogUI.controller.signal});
  const plan=await catalogReadPlan(response);if(catalogUI.version!==version){catalogPost("/api/catalog/fold-discard",{plan:plan.id}).catch(()=>{});return;}catalogUI.plan=plan;catalogUI.selected=new Set(catalogUI.plan.groups.filter(g=>g.status==="ready").map(g=>g.id));
  catalogRenderPlan();
 }catch(error){catalogShowError(error);}finally{catalogUI.busy=false;}
}
function catalogGroupReason(pg){if(pg.status==="review"&&pg.conflicts?.length)return "Choose the saved values to keep: "+[...new Set(pg.conflicts.map(key=>catalogLabels[key.split(".")[0]]||key))].join(", ")+ ".";return (pg.reason||"").replace("confirm regional titles, edition and content with --interactive","compare the regional titles, edition and content before confirming");}
function catalogReviewHint(pg){
 if(pg.status==="blocked")return "This group cannot be merged. Field edits or identity confirmation cannot override the conflict. Check the reason above, resolve the underlying card or file-access problem, then build a new plan.";
 if(pg.conflicts?.length)return "Choose the highlighted saved values, then preview the result.";
 if(pg.needs_identity)return "Compare the regional titles, edition and content before confirming that these cards represent the same game.";
 return "Preview the result to inspect the merged card.";
}
function catalogRenderPlan(){
 catalogUI.mode="plan";catalogUI.group=null;catalogUI.dirty=false;
 const plan=catalogUI.plan;if(!plan)return;catalogHeader("Review duplicates",plan.platform+" · Suggested groups and primary cards are already prepared.");
 const groups=plan.groups;const count=status=>groups.filter(g=>g.status===status).length;
 $("catalog-content").innerHTML='<div class="catalog-metrics">'+["ready","review","blocked"].map(status=>'<div class="catalog-metric '+status+'"><strong>'+count(status)+'</strong><span>'+catalogStatus[status]+'</span></div>').join("")+'<div id="catalog-feedback"></div>'+
 (groups.length&&count("blocked")===groups.length?catalogNotice("No groups are ready to merge. Compare lets you inspect why each group is blocked; it does not approve a merge.","warn"):"")+
 (groups.length?'<div class="catalog-plan-list">'+groups.map(pg=>{
  const g=pg.group;const names=[g.Keep,...(g.Merge||[])].map(x=>x.Folder);
  return '<article class="catalog-plan-row"><input type="checkbox" aria-label="Include '+escapeAttr(g.Keep.Title||g.Keep.Folder)+'" data-plan-select="'+pg.id+'" '+(catalogUI.selected.has(pg.id)?'checked ':'')+(pg.status!=="ready"?'disabled':'')+'><div><div class="catalog-row-title"><h3>'+cEscape(g.Keep.Title||g.Keep.Folder)+'</h3><span class="catalog-badge '+pg.status+'">'+catalogStatus[pg.status]+'</span></div><p>'+cEscape(names.join(" + "))+'</p><p>'+cEscape(catalogGroupReason(pg)||"Primary chosen from catalog preferences. Releases and media will be retained.")+'</p></div><button class="tab-button" data-plan-review="'+pg.id+'">Compare</button></article>';
 }).join("")+'</div><div class="catalog-actions"><button class="catalog-primary" id="catalog-apply-ready">Apply ready groups</button><span class="secondary" id="catalog-selected-count"></span></div>':'<div class="catalog-empty"><div class="catalog-saved">✓</div>No duplicate groups found for this platform.<p>You can also select cards in the library to compare them.</p></div>');
 document.querySelectorAll("[data-plan-select]").forEach(input=>input.addEventListener("change",()=>{const id=Number(input.dataset.planSelect);if(input.checked)catalogUI.selected.add(id);else catalogUI.selected.delete(id);catalogSelectionCount();}));
 document.querySelectorAll("[data-plan-review]").forEach(button=>button.addEventListener("click",()=>catalogReviewGroup(Number(button.dataset.planReview))));
 $("catalog-apply-ready")?.addEventListener("click",()=>catalogApply([...catalogUI.selected].map(id=>({id,revision:plan.groups.find(g=>g.id===id).revision,action:"merge"}))));
 catalogSelectionCount();
}
function catalogSelectionCount(){const n=catalogUI.selected.size;const button=$("catalog-apply-ready");if(button){button.textContent="Apply ready groups ("+n+")";button.disabled=n===0;}$("catalog-selected-count")?.replaceChildren(document.createTextNode("Only selected, ready groups will change."));}
function catalogCandidate(card,index,fields){
 const g=card.game,p=card.profile||{},snapshot=card.edit.fields,cover=bestCover(card.local_media||[]),primary=g.folder===catalogUI.group.group.Keep.Folder;
 return '<article class="catalog-candidate '+(primary?'is-primary':'')+'">'+(cover?'<img src="'+escapeAttr(cover.url)+'" alt="'+escapeAttr(p.title||g.folder)+'">':'')+'<span class="catalog-badge">'+(primary?"Suggested primary":"Candidate")+'</span><h3>'+cEscape(p.title||g.folder)+'</h3><p class="secondary">'+cEscape(g.folder)+'</p>'+
 (catalogUI.group.group.NativeDigest?'':'<button class="tab-button" data-candidate-primary="'+index+'" '+(primary||catalogUI.group.status==="blocked"?'disabled':'')+'>Use as primary</button>')+
 fields.map(key=>{
  const values=catalogUI.cards.map(c=>catalogFieldValue(key,c.edit.fields,c.profile));const differs=new Set(values).size>1;const local=Object.prototype.hasOwnProperty.call(snapshot,key);
  return '<div class="catalog-field '+(differs?'different':'')+'"><div class="catalog-field-label"><span>'+cEscape(catalogLabels[key])+(differs?' · differs':'')+' · '+(local?'saved':'source')+'</span>'+(local&&key!=="title"&&!catalogUI.group.group.NativeDigest&&catalogUI.group.status!=="blocked"?'<button class="tab-button" data-candidate-value="'+index+'" data-value-key="'+key+'">Use value</button>':'')+'</div><div class="catalog-field-value">'+cEscape(values[index]||"—")+'</div></div>';
 }).join("")+'<details><summary>Releases, media & evidence</summary>'+renderReleases(p.releases||g.releases||[])+'<p>'+cEscape((card.local_media||[]).length)+" local media files"+'</p><button class="tab-button" data-candidate-inspect="'+index+'">Inspect full card</button><pre>'+cEscape(card.catalog_yaml||"")+'</pre></details></article>';
}
async function catalogReviewGroup(id){
 if(catalogUI.busy)return;
 catalogUI.busy=true;catalogUI.mode="review";catalogUI.primary="";catalogUI.patch={};catalogUI.dirty=false;catalogUI.group=catalogUI.plan.groups.find(g=>g.id===id);
 catalogOpen("Compare games","Loading candidates and checking merge status…");const version=catalogUI.version;
 try{
  const refs=[catalogUI.group.group.Keep,...(catalogUI.group.group.Merge||[])];
  catalogUI.cards=await Promise.all(refs.map(async ref=>{
   const query=new URLSearchParams({platform:catalogUI.platform,game:ref.Folder});
   const [card,edit]=await Promise.all([fetchJSON("/api/game?"+query),fetchJSON("/api/catalog/edit?"+query)]);return {...card,edit};
  }));
  if(catalogUI.version!==version)return;
  const response=await catalogPost("/api/catalog/fold-preview",{plan:catalogUI.plan.id,group:id});
  if(catalogUI.version!==version)return;
  catalogAcceptGroup(response.group);catalogUI.preview=response.preview;catalogUI.patch=response.draft?.fields||{};
  const primary=catalogUI.cards.find(c=>c.game.folder===response.group.group.Keep.Folder);
  catalogUI.base=response.group.result?.fields||primary.edit.fields;catalogUI.formProfile=response.preview?.profile||primary.profile;
  catalogRenderReview();
 }catch(error){catalogShowError(error);}finally{catalogUI.busy=false;}
}
function catalogAcceptGroup(group){
 catalogUI.group=group;const i=catalogUI.plan.groups.findIndex(g=>g.id===group.id);catalogUI.plan.groups[i]=group;
 if(group.status==="ready")catalogUI.selected.add(group.id);else catalogUI.selected.delete(group.id);
}
function catalogRenderReview(){
 const pg=catalogUI.group,g=pg.group,blocked=pg.status==="blocked";
 catalogHeader("Compare games",catalogUI.cards.length+" cards · "+catalogStatus[pg.status]+". "+(blocked?"Preview and apply are unavailable. Inspect the reason below.":"Review the proposed result; source observations and releases are retained."));
 $("catalog-content").innerHTML='<button class="catalog-back" id="catalog-back">← Back to plan</button><div id="catalog-feedback">'+(pg.reason?catalogNotice(catalogGroupReason(pg),pg.status==="blocked"?"error":"warn"):'')+'</div><div class="catalog-comparison"><section class="catalog-candidate-area"><p class="catalog-scroll-hint">Scroll sideways to compare cards →</p><div class="catalog-candidates" tabindex="0" role="region" aria-label="Candidate cards; scroll horizontally to compare">'+catalogUI.cards.map((card,i)=>catalogCandidate(card,i,["title","alternate_titles","description","descriptions","developer","publisher","release_date","genres","players"])).join("")+'</div></section><section class="catalog-result"><span class="catalog-eyebrow">'+(blocked?"Merge status":"Result")+'</span><h3>'+cEscape(blocked?"Merge blocked":g.Keep.Title||g.Keep.Folder)+'</h3>'+
 (blocked?'':g.NativeDigest?'<p>MAME chooses its primary from the accepted snapshot. Edit individual cards in the library before rebuilding this plan.</p>':'<details><summary>Correct the result</summary><div id="catalog-form-host"></div></details>')+
 (!blocked&&pg.needs_identity?'<label class="catalog-confirm"><input type="checkbox" id="catalog-confirm-identity" '+(g.ReviewAccepted?'checked':'')+'>These cards represent the same game.</label>'+(pg.manual?'<label class="catalog-form">Evidence for this decision<input id="catalog-identity-reason" value="'+escapeAttr(g.ReviewReason||"")+'" placeholder="Why these releases belong together"></label>':''):'')+
 '<div class="catalog-actions"><button class="tab-button" id="catalog-preview-group" '+(blocked?'disabled':'')+'>Preview result</button><button class="catalog-primary" id="catalog-commit" '+(pg.status!=="ready"||catalogUI.dirty?'disabled':'')+'>Apply this merge</button></div><p class="secondary" id="catalog-preview-feedback">'+(blocked?"Merge unavailable for this group.":catalogUI.dirty?"Changes need a new preview.":pg.status==="review"?"A decision is needed before applying.":"Result matches the current preview.")+'</p><div id="catalog-result-preview">'+catalogProfilePreview(catalogUI.preview,catalogReviewHint(pg))+'</div><details><summary>Why this group?</summary>'+(g.Evidence||[]).map(e=>'<p>'+cEscape(e.Source+": "+e.Field+" · "+e.Value)+'</p>').join("")+'</details><details><summary>Affected files</summary><pre>'+cEscape((g.FileChanges||[]).map(f=>f.Action+": "+f.FromFolder+"/"+f.Path+" → "+f.Target).join("\n")||"Catalog documents and merged card folders")+'</pre></details>'+
 '<div class="catalog-actions"><button class="tab-button" id="catalog-keep-separate" '+(!g.Evidence?.length||g.NativeDigest?'disabled':'')+'>Keep separate</button></div></section></div>';
 catalogRenderForm();
 $("catalog-back").addEventListener("click",()=>{if(!catalogUI.dirty||window.confirm("Discard unpreviewed changes?"))catalogRenderPlan();});
 $("catalog-preview-group").addEventListener("click",catalogPreviewGroup);
 $("catalog-commit").addEventListener("click",()=>catalogApply([{id:pg.id,revision:catalogUI.group.revision,action:"merge"}]));
 $("catalog-keep-separate").addEventListener("click",()=>catalogApply([{id:pg.id,revision:catalogUI.group.revision,action:"separate"}]));
 $("catalog-confirm-identity")?.addEventListener("change",catalogDirty);$("catalog-identity-reason")?.addEventListener("input",catalogDirty);
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
async function catalogPreviewGroup(){
 if(catalogUI.busy)return;catalogUI.busy=true;const version=catalogUI.version;
 try{
  const response=await catalogPost("/api/catalog/fold-preview",{plan:catalogUI.plan.id,group:catalogUI.group.id,draft:{primary:catalogUI.primary||catalogUI.group.group.Keep.Folder,fields:catalogUI.patch,confirm:$("catalog-confirm-identity")?.checked||false,reason:$("catalog-identity-reason")?.value||""}});
  catalogAcceptGroup(response.group);if(catalogUI.version!==version){catalogUI.dirty=true;return;}catalogUI.preview=response.preview;catalogUI.dirty=false;catalogUI.primary="";catalogRenderReview();
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
async function catalogApply(decisions){
 if(catalogUI.busy||!decisions.length)return;catalogUI.busy=true;catalogUI.mode="applying";
 catalogHeader("Applying reviewed changes","Keep this window open while the selected groups are saved.");
 $("catalog-content").innerHTML='<div class="catalog-progress"><span class="catalog-spinner"></span><span>Merging selected cards and preserving their releases and media…</span></div>';
 try{
  const response=await catalogPost("/api/catalog/fold-apply",{plan:catalogUI.plan.id,decisions});catalogUI.plan=null;catalogUI.dirty=false;
  catalogHeader("Catalog updated",catalogUI.platform);
  $("catalog-content").innerHTML='<div class="catalog-empty"><div class="catalog-saved">✓</div><h3>'+response.result.applied+' groups merged · '+response.result.kept_separate+' kept separate</h3><p>Open the library to inspect your saved cards.</p></div>'+(response.warning?catalogNotice(response.warning,"warn"):'')+(response.result.backups?.length?'<details class="raw-details"><summary>Recovery locations</summary><pre>'+cEscape(response.result.backups.join("\n"))+'</pre></details>':'');
  catalogUI.compare.clear();catalogUpdateTools();await catalogRefreshLibrary();
 }catch(error){if(error.consumed)catalogUI.plan=null;catalogHeader("Review needed","Build a new plan before retrying.");catalogShowError(error);if(error.result?.applied||error.result?.kept_separate){$("catalog-content").insertAdjacentHTML("beforeend",catalogNotice("Completed before the stop: "+error.result.applied+" merged; "+error.result.kept_separate+" kept separate.","warn"));await catalogRefreshLibrary();}}finally{catalogUI.busy=false;catalogUI.mode="done";}
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
catalogCapabilities();
