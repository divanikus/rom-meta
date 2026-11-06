/* Native export on the serve host. Plans and progress are owned by Operations. */
const nativeExportUI={reviewDrafts:new Map(),loaded:false,loading:false,busy:false,error:"",revision:"",profilesRevision:"",profiles:[],destinations:[],host:"",os:"",draft:null,folder:null,folderVersion:0};
const exportStates={planning:"Preparing export plan",ready:"Ready to export",needs_review:"Conflicts to resolve",exporting:"Exporting",succeeded:"Complete",incomplete:"Stopped after partial write",failed:"Could not finish"};
function exportStageLabel(stage){const labels={catalog:"Opening library",checking:"Checking reviewed inputs",starting:"Starting",planned:"Plan ready",complete:"Complete",write:"Writing files","native fields":"Preparing frontend metadata","read destination":"Reading destination"};return labels[stage]||((stage||"Starting").replace(/^./,s=>s.toUpperCase()));}
function exportProgressUnit(stage){
 const part=(stage||"").split(" / ").at(-1);
 return ({"ROMs":"ROM files","hash ROMs":"ROM files",write:"files",index:"cards",reindexing:"cards","catalog-index":"cards","metadata/media":"ROM entries","plan library/media":"games"})[part]||"items";
}
function exportDraft(){return {id:"",name:"",target:state.exportSettings?.target||"es-de",path:"",rom_root:"",media_root:"",platform:state.platform||"",all:false,profile_yaml:"",profile_name:"Library settings",profile_mode:"defaults"}}
async function exportOpen(){
 if(!nativeExportUI.draft)nativeExportUI.draft=exportDraft();
 exportRender();
 if(!nativeExportUI.loaded&&!nativeExportUI.loading)await exportLoadDestinations();
 await exportEnsurePolicy();
}
async function exportLoadDestinations(){
 nativeExportUI.loading=true;nativeExportUI.error="";exportRender();
 try{const [data]=await Promise.all([fetchJSON("/api/export/destinations"),exportLoadProfiles()]);Object.assign(nativeExportUI,data,{loaded:true});}
 catch(error){nativeExportUI.error=error.message;}
 finally{nativeExportUI.loading=false;if(state.view==="export")exportRender();}
}
function exportCapture(){
 const f=nativeExportUI.draft;if(!f||!$("export-run-form"))return;
 for(const [key,id]of Object.entries({name:"export-destination-name",target:"export-run-target",path:"export-destination-path",rom_root:"export-rom-root",platform:"export-run-platform",media_root:"export-media-root"})){const el=$(id);if(el)f[key]=el.value;}
 const profileName=$("export-saved-profile-name");if(profileName)f.savedProfileName=profileName.value;
 f.all=$("export-rom-mode")?.value==="all";
}
function exportPathField(label,id,key,hint){
 const value=nativeExportUI.draft[key]||"";
 return '<label>'+label+'<span class="export-path-control"><input id="'+id+'" value="'+escapeAttr(value)+'" placeholder="Absolute path on the server"><button type="button" class="tab-button" data-export-browse="'+key+'">Browse…</button></span>'+(hint?'<small>'+hint+'</small>':"")+'</label>';
}
function exportRender(){
 const pane=$("native-export-pane");if(!pane)return;
 if(!catalogUI.enabled){pane.innerHTML='<h1>Export</h1><p>Start serve with --edit to save destinations and run exports on this machine.</p><button class="tab-button" data-export-preview>Preview profile</button>';exportBind();return;}
 const f=nativeExportUI.draft||exportDraft(),busy=nativeExportUI.busy||nativeExportUI.loading||f.profileLoading;
 const platforms=state.platforms||[];
 pane.innerHTML='<div class="management-heading"><div><span class="operation-eyebrow">EXPORT TO A FRONTEND</span><h1>Export</h1><p>Write metadata, playlists and media for your existing ROMs.</p></div></div>'+
 '<p class="secondary export-host">Folders belong to <strong>'+escapeHTML(nativeExportUI.host||"the machine running serve")+'</strong>'+ (nativeExportUI.os?' · '+escapeHTML(nativeExportUI.os):"")+'. ROM files stay in place.</p>'+
 (nativeExportUI.error?'<p class="operation-error" role="alert">'+escapeHTML(nativeExportUI.error)+'</p>':"")+
 '<form id="export-run-form"><fieldset '+(busy?'disabled':'')+'><div class="export-setup-grid"><section class="operation-detail"><h2>Destination</h2><label>Saved destination<select id="export-destination-select"><option value="">New destination</option>'+
 nativeExportUI.destinations.map(d=>'<option value="'+escapeAttr(d.id)+'" '+(f.id===d.id?'selected':'')+'>'+escapeHTML(d.name)+'</option>').join("")+'</select></label>'+
 '<div class="export-inline"><label>Name<input id="export-destination-name" placeholder="My LaunchBox" value="'+escapeAttr(f.name)+'"></label><label>Frontend<select id="export-run-target"><option value="es-de" '+(f.target==="es-de"?'selected':'')+'>ES-DE</option><option value="launchbox" '+(f.target==="launchbox"?'selected':'')+'>LaunchBox</option></select></label></div>'+
 exportPathField("Frontend folder","export-destination-path","path",f.target==="launchbox"?"The LaunchBox installation folder containing Data/Platforms.":"The ES-DE data folder containing gamelists and downloaded_media.")+
 (f.target==="es-de"?'<details '+(f.media_root?'open':'')+'><summary>Separate media folder</summary>'+exportPathField("Media folder (optional)","export-media-root","media_root","Leave empty to use downloaded_media inside the frontend folder.")+'</details>':"")+
 '<div class="operation-actions"><button type="button" class="tab-button" id="export-save-destination">Save destination</button>'+(f.id?'<button type="button" class="tab-button" id="export-delete-destination">Remove saved destination</button>':"")+'<button type="button" class="tab-button" id="export-reload-destinations">Reload saved</button></div><small>Saving stores these paths and profile choices. It does not export files.</small></section>'+
 '<section class="operation-detail"><h2>ROMs and profile</h2>'+exportPathField("ROM folder","export-rom-root","rom_root",f.all?"A parent folder containing platform folders, for example nes, snes and psx.":"The folder containing ROMs for this platform.")+
 '<div class="export-inline"><label>Folder layout<select id="export-rom-mode"><option value="single" '+(!f.all?'selected':'')+'>One platform</option><option value="all" '+(f.all?'selected':'')+'>All platform folders</option></select></label>'+
 (!f.all?'<label>Platform<select id="export-run-platform"><option value="">Choose platform</option>'+platforms.map(p=>'<option value="'+escapeAttr(p.platform)+'" '+(p.platform===f.platform?'selected':'')+'>'+escapeHTML(p.platform)+'</option>').join("")+'</select></label>':"")+'</div>'+
 exportProfileEditor(f)+'</section></div><div class="export-prepare-bar"><button class="catalog-primary" type="submit" '+(!f.policy||f.profileRaw!==undefined?'disabled':'')+'>'+(nativeExportUI.busy?'Preparing…':'Prepare export plan')+'</button><p>Scan ROMs and review the proposed changes before applying. Progress stays in Operations after you close the browser.</p></div></fieldset></form>';
 exportBind();
}
async function exportDestinationInput(){
 exportCapture();const f=nativeExportUI.draft,d=Object.fromEntries(["id","name","target","path","rom_root","media_root","platform","all","profile_yaml","profile_name","profile_id"].map(k=>[k,f[k]]));
 if(f.policy){const result=await exportResolvePolicy(f);d.profile_yaml=result.profile_yaml;d.profile_name=f.profile_name||"Custom profile";}
 else if(f.profile_mode==="defaults"&&!f.profile_yaml){d.profile_yaml="";d.profile_name="Library settings";}
 if(f.profile_mode==="yaml"&&!d.profile_yaml)throw Error("Choose a YAML profile first.");
 if(d.all)d.platform="";
 if(d.target!=="es-de")d.media_root="";
 return d;
}
async function exportSaveDestination(remove=false){
 if(nativeExportUI.busy)return;
 exportCapture();nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
 try{
  const fallbackPolicy=nativeExportUI.draft.policy;
  const d=remove?{id:nativeExportUI.draft.id}:await exportDestinationInput();
  if(remove&&!window.confirm("Remove this saved destination? Files in the destination are kept."))return;
  nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
  const data=await catalogPost("/api/export/destinations",{revision:nativeExportUI.revision,destination:d,delete:remove});
  Object.assign(nativeExportUI,data);
  if(remove)nativeExportUI.draft=exportDraft();
  else{const saved=data.destinations.find(x=>x.id===d.id||x.name===d.name);nativeExportUI.draft={...saved,policy:fallbackPolicy,profile_mode:saved.profile_id||saved.profile_yaml?"custom":"defaults"};}
 }catch(error){nativeExportUI.error=error.message;}
 finally{nativeExportUI.busy=false;exportRender();}
}
async function exportPrepare(event){
 event?.preventDefault();if(nativeExportUI.busy)return;
 exportCapture();nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
 try{
  const d=await exportDestinationInput();d.profile_id="";exportRender();
  const op=await catalogPost("/api/export/prepare",{destination:d});
  libraryRememberJob(op);libraryNavigate("operations");libraryPollOperations();
 }catch(error){nativeExportUI.error=error.message;exportRender();}
 finally{nativeExportUI.busy=false;if(state.view==="export")exportRender();}
}
function exportBind(){
 exportBindProfile();
 document.querySelectorAll("[data-export-preview]").forEach(b=>b.addEventListener("click",exportPreviewProfile));
 $("export-run-form")?.addEventListener("submit",exportPrepare);
 $("export-run-form")?.addEventListener("input",exportCapture);
 for(const id of ["export-run-target","export-rom-mode"])$(id)?.addEventListener("change",()=>{exportCapture();if(id==="export-run-target"){nativeExportUI.draft.profile_id="";nativeExportUI.draft.editProfileID="";nativeExportUI.draft.savedProfileName="";}exportRender();});
 $("export-destination-select")?.addEventListener("change",event=>{
  const saved=nativeExportUI.destinations.find(d=>d.id===event.target.value);
  nativeExportUI.draft=saved?{...saved,profile_mode:saved.profile_yaml?"yaml":"defaults"}:exportDraft();nativeExportUI.error="";exportRender();exportEnsurePolicy();
 });
 $("export-save-destination")?.addEventListener("click",()=>exportSaveDestination());
 $("export-delete-destination")?.addEventListener("click",()=>exportSaveDestination(true));
 $("export-reload-destinations")?.addEventListener("click",()=>{exportCapture();exportLoadDestinations();});
 $("export-profile-upload")?.addEventListener("change",async event=>{
  const file=event.target.files?.[0];if(!file)return;
  const draft=nativeExportUI.draft;
  try{if(file.size>1048576)throw Error("Profile must be at most 1 MiB.");const content=await file.text();if(nativeExportUI.draft!==draft)return;draft.profile_id="";draft.editProfileID="";draft.savedProfileName="";draft.profile_yaml=content;draft.profile_name=file.name;draft.profile_mode="yaml";delete draft.policy;delete draft.profileRaw;nativeExportUI.error="";await exportEnsurePolicy();}
  catch(error){nativeExportUI.error=error.message;}exportRender();
 });
 document.querySelectorAll("[data-export-browse]").forEach(b=>b.addEventListener("click",()=>{exportCapture();exportBrowse(b.dataset.exportBrowse,nativeExportUI.draft[b.dataset.exportBrowse]);}));
}
async function exportBrowse(field,path=""){
 const version=++nativeExportUI.folderVersion;
 nativeExportUI.folder={field,path,loading:true,error:"",folders:[]};exportRenderFolder();
 try{const data=await fetchJSON("/api/export/folders?"+new URLSearchParams({path}));if(version!==nativeExportUI.folderVersion)return;nativeExportUI.folder={field,...data};}
 catch(error){if(version!==nativeExportUI.folderVersion)return;nativeExportUI.folder={field,path,error:error.message,folders:[]};}
 exportRenderFolder();
}
function exportCloseFolder(){nativeExportUI.folderVersion++;nativeExportUI.folder=null;$("export-folder-dialog").classList.add("hidden");$("app").inert=false;}
function exportRenderFolder(){
 const f=nativeExportUI.folder,dialog=$("export-folder-dialog");if(!f)return;
 dialog.classList.remove("hidden");$("app").inert=true;
 dialog.innerHTML='<div class="export-folder-box"><div class="management-heading"><h2>Choose a server folder</h2><button class="tab-button" id="export-folder-close">Close</button></div><form id="export-folder-path-form" class="export-path-control"><input id="export-folder-path" aria-label="Server folder path" value="'+escapeAttr(f.path||"")+'"><button class="tab-button">Open</button></form>'+
 (f.error?'<p class="operation-error" role="alert">'+escapeHTML(f.error)+'</p>':"")+
 '<div class="operation-actions"><button class="tab-button" id="export-folder-up">Up</button><button class="catalog-primary" id="export-folder-use" '+(!f.path||f.loading||f.error?'disabled':'')+'>Use this folder</button></div>'+
 '<div class="export-folder-list">'+(f.loading?'<p>Reading folders…</p>':f.folders.length?f.folders.map((x,i)=>'<button class="tab-button" data-export-folder="'+i+'">▸ '+escapeHTML(x.name)+'</button>').join(""):'<p class="secondary">No visible subfolders. A new destination path can be entered in the export form.</p>')+'</div></div>';
 $("export-folder-close").addEventListener("click",exportCloseFolder);
 $("export-folder-path-form").addEventListener("submit",e=>{e.preventDefault();exportBrowse(f.field,$("export-folder-path").value);});
 $("export-folder-up").addEventListener("click",()=>exportBrowse(f.field,f.parent||""));
 $("export-folder-use").addEventListener("click",()=>{nativeExportUI.draft[f.field]=f.path;exportCloseFolder();exportRender();});
 dialog.querySelectorAll("[data-export-folder]").forEach(b=>b.addEventListener("click",()=>exportBrowse(f.field,f.folders[Number(b.dataset.exportFolder)].path)));
}
function nativeExportDetail(job){
 const x=job.export,d=x.destination,n=x.totals,p=x.progress||{},busy=operationBusy(job),review=["ready","needs_review"].includes(job.state);
 return '<article class="operation-detail"><div class="operation-eyebrow">FRONTEND EXPORT · '+operationStatus(job.state,exportStates[job.state]||job.state)+'</div><h2>'+escapeHTML(d.name)+'</h2>'+
 '<dl class="operation-facts"><dt>Frontend</dt><dd>'+escapeHTML(d.target)+'</dd><dt>Destination</dt><dd>'+escapeHTML(d.path)+'</dd><dt>ROMs</dt><dd>'+escapeHTML(d.rom_root)+'</dd><dt>Scope</dt><dd>'+escapeHTML(d.all?"All platform folders":d.platform)+'</dd><dt>Profile</dt><dd>'+escapeHTML(d.profile_name)+'</dd>'+(d.media_root?'<dt>Media</dt><dd>'+escapeHTML(d.media_root)+'</dd>':"")+'</dl>'+
 (busy?'<div class="operation-progress" role="status"><strong>'+escapeHTML(exportStageLabel(p.Stage))+'</strong><span>'+escapeHTML(p.Item||"")+'</span>'+operationProgressMeter(p.Current,p.Total,exportProgressUnit(p.Stage))+'<p>You can close the browser and return to this operation.</p></div>':"")+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+
 (n?'<div class="operation-summary"><div><strong>'+formatNumber(n.matched)+'</strong><span>matched ROM entries</span></div><div><strong>'+formatNumber(n.files)+'</strong><span>files in the plan</span></div><div><strong>'+formatNumber(x.written)+'</strong><span>files written</span></div></div><p class="secondary">'+formatNumber(n.unmatched)+' unmatched · '+formatNumber(n.ambiguous)+' ambiguous · '+formatNumber(n.unsupported)+' unsupported</p>'+
 (n.conflicts?.length?'<div class="operation-error"><strong>Resolve conflicts and prepare a new plan</strong><ul>'+n.conflicts.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></div>':"")+
 (n.warnings?.length?'<details><summary>Warnings ('+n.warnings.length+')</summary><ul>'+n.warnings.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></details>':"")+
 (n.skipped?.length?'<details><summary>Skipped platforms ('+n.skipped.length+')</summary><ul>'+n.skipped.map(v=>'<li>'+escapeHTML(v.system)+': '+escapeHTML(v.reason)+'</li>').join("")+'</ul></details>':"")+
 exportIssues(job):"")+
 (review?'<p>The destination is unchanged. Apply writes the reviewed files using the normal additive export rules. Close the frontend before applying.</p>':job.state==="succeeded"?'<p>'+(x.written?"Export completed.":"The destination is already up to date; no files were written.")+'</p>':job.state==="incomplete"?'<p>Some files were written before the error. Prepare another plan against the current destination.</p>':"")+
 '<div class="operation-actions">'+(job.state==="ready"?'<button class="catalog-primary" data-native-export-apply="'+job.id+'">'+(n?.files?"Apply export":"Finish — already up to date")+'</button>':"")+
 (review?'<a class="tab-button" href="/api/export/plan?id='+encodeURIComponent(job.id)+'" download>Download full plan</a>':"")+
 (!busy?'<button class="tab-button" data-native-export-again>'+(["failed","incomplete"].includes(job.state)?"Retry — review a new plan":"Prepare another plan")+'</button><button class="tab-button" data-import-dismiss="'+job.id+'">Dismiss</button>':"")+'</div></article>';
}
function nativeExportBindOperation(job){
 if(job?.kind!=="export")return;
 exportBindReview(job);
 document.querySelectorAll("[data-native-export-apply]").forEach(b=>b.addEventListener("click",async()=>{if(exportReviewDirty(job))return;b.disabled=true;await libraryImportAction("/api/export/apply",{id:job.id,revision:job.revision});}));
 document.querySelectorAll("[data-native-export-again]").forEach(b=>b.addEventListener("click",()=>{nativeExportUI.draft={...job.export.destination,profile_mode:"yaml"};nativeExportUI.error="";libraryNavigate("export");}));
}

function exportIssueDraft(job){
 let d=nativeExportUI.reviewDrafts.get(job.id);
 if(!d||d.revision!==job.revision){d={revision:job.revision,excluded:new Set(job.export.excluded||[]),query:"",page:0};nativeExportUI.reviewDrafts.set(job.id,d);}
 return d;
}
function exportReviewDirty(job){const d=exportIssueDraft(job),saved=job.export.excluded||[];return d.excluded.size!==saved.length||saved.some(id=>!d.excluded.has(id));}
function exportIssues(job){
 const issues=job.export.totals?.issues||[],d=exportIssueDraft(job),review=["ready","needs_review"].includes(job.state),q=d.query.toLowerCase();
 const filtered=issues.filter(x=>[x.rom,x.game,x.reason,x.platform,...(x.candidates||[])].join(" ").toLowerCase().includes(q)),pages=Math.max(1,Math.ceil(filtered.length/40));d.page=Math.min(d.page,pages-1);
 return '<section class="export-review"><h3>Inputs needing attention ('+issues.length+')</h3>'+
 (!issues.length?'<p>No unresolved or ambiguous ROMs. Recognized inputs are included automatically.</p>':'<p>Review filenames and match evidence. You can exclude inputs from this export; ROMs and library cards stay unchanged.</p><div class="export-issue-tools"><input id="export-issue-search" aria-label="Filter problem inputs" placeholder="Filter filenames or reasons" value="'+escapeAttr(d.query)+'">'+(review?'<button type="button" class="tab-button" data-export-exclude-all>Exclude all unresolved</button><button type="button" class="tab-button" data-export-restore-all>Restore all</button>':'')+'</div><div class="export-issues">'+filtered.slice(d.page*40,(d.page+1)*40).map(x=>'<div class="export-issue"><label><input type="checkbox" data-export-exclude="'+escapeAttr(x.id)+'" '+(d.excluded.has(x.id)?'checked ':'')+(!review?'disabled ':'')+'aria-label="'+escapeAttr("Exclude "+x.rom)+'"><span>Exclude <strong>'+escapeHTML(x.rom)+'</strong> · '+escapeHTML(x.status)+'</span></label><small>'+escapeHTML(x.platform)+' · '+escapeHTML(x.reason||x.warnings?.join("; ")||"Review match evidence")+'</small>'+(x.game?'<small>Matched: '+escapeHTML(x.game)+(x.release?' / '+escapeHTML(x.release):"")+(x.matched_by?' · '+escapeHTML(x.matched_by):"")+'</small>':"")+(x.candidates?.length?'<small>Candidates: '+escapeHTML(x.candidates.join("; "))+'</small>':"")+'</div>').join("")+'</div><div class="operation-actions">'+(pages>1?'<button class="tab-button" data-export-issue-page="-1" '+(!d.page?'disabled':'')+'>Previous</button><span>'+(d.page+1)+' / '+pages+' · '+filtered.length+' inputs</span><button class="tab-button" data-export-issue-page="1" '+(d.page===pages-1?'disabled':'')+'>Next</button>':"")+(review?'<button class="tab-button" data-export-review-update '+(!exportReviewDirty(job)?'disabled':'')+'>Update plan</button>':'')+'<span id="export-review-hint" class="secondary">'+(exportReviewDirty(job)?'Update the plan to review these exclusions.':d.excluded.size+' excluded from this export.')+'</span></div>')+'</section>';
}
function exportBindReview(job){
 const d=exportIssueDraft(job);
 const update=()=>{document.querySelectorAll("[data-native-export-apply]").forEach(b=>b.disabled=exportReviewDirty(job));document.querySelectorAll("[data-export-review-update]").forEach(b=>b.disabled=!exportReviewDirty(job));const hint=$("export-review-hint");if(hint)hint.textContent=exportReviewDirty(job)?"Update the plan to review these exclusions.":d.excluded.size+" excluded from this export.";};
 document.querySelectorAll("[data-export-exclude]").forEach(c=>c.addEventListener("change",()=>{c.checked?d.excluded.add(c.dataset.exportExclude):d.excluded.delete(c.dataset.exportExclude);update();}));
 document.querySelectorAll("[data-export-exclude-all]").forEach(b=>b.addEventListener("click",()=>{for(const x of job.export.totals.issues||[])if(x.status!=="matched")d.excluded.add(x.id);libraryRenderOperations();}));
 document.querySelectorAll("[data-export-restore-all]").forEach(b=>b.addEventListener("click",()=>{d.excluded.clear();libraryRenderOperations();}));
 document.querySelectorAll("[data-export-issue-page]").forEach(b=>b.addEventListener("click",()=>{d.page+=Number(b.dataset.exportIssuePage);libraryRenderOperations();}));
 $("export-issue-search")?.addEventListener("input",event=>{d.query=event.target.value;d.page=0;const pos=event.target.selectionStart;event.target.blur();libraryRenderOperations();$("export-issue-search")?.focus();$("export-issue-search")?.setSelectionRange(pos,pos);});
 document.querySelectorAll("[data-export-review-update]").forEach(b=>b.addEventListener("click",async()=>{b.disabled=true;await libraryImportAction("/api/export/review",{id:job.id,revision:job.revision,excluded:[...d.excluded]});}));
 update();
}



const exportArtworkVariants={original:["original","unknown","reconstructed","fanart"],reconstructed:["reconstructed","original","unknown","fanart"]};
const exportArtworkOrder=["region","variant","source","format"];
function exportArtworkPreference(d){
 if(d.artworkPreference!==undefined)return d.artworkPreference;
 const rules=Object.entries(d.policy.selection.media).filter(([role])=>role!=="video"&&role!=="manual").map(([,rule])=>rule);
 return Object.keys(exportArtworkVariants).find(key=>rules.length&&rules.every(rule=>JSON.stringify(rule.variants)===JSON.stringify(exportArtworkVariants[key])&&JSON.stringify(rule.order)===JSON.stringify(exportArtworkOrder)))||"auto";
}
function exportApplyArtwork(d,role,rule,preference){
 if(role==="video"||role==="manual")return;
 if(preference==="auto"){
  const base=d.artworkBase?.[role];if(!base)return;
  rule.order=[...base.order];
  if(Object.hasOwn(base,"variants"))rule.variants=base.variants===null?null:[...base.variants];else delete rule.variants;
  return;
 }
 d.artworkBase=d.artworkBase||{};
 if(!Object.hasOwn(d.artworkBase,role))d.artworkBase[role]=JSON.parse(JSON.stringify({order:rule.order,variants:rule.variants}));
 rule.variants=[...exportArtworkVariants[preference]];rule.order=[...exportArtworkOrder];
}
function exportChangeArtwork(preference){
 const d=nativeExportUI.draft;if(!d?.policy||d.profileRaw!==undefined||!["auto",...Object.keys(exportArtworkVariants)].includes(preference))return;
 exportCapture();d.artworkPreference=preference;
 for(const [role,rule]of Object.entries({...d.disabledMedia,...d.policy.selection.media}))exportApplyArtwork(d,role,rule,preference);
 exportPolicyChanged();exportRender();
}
function exportInstallPolicy(d,result){
 d.policy=result.profile;d.profile_yaml=result.profile_yaml;
 delete d.artworkPreference;delete d.artworkBase;delete d.disabledMedia;
}

async function exportLoadProfiles(){
 const data=await fetchJSON("/api/export/profiles");
 nativeExportUI.profiles=data.profiles||[];nativeExportUI.profilesRevision=data.revision||"";
}
function exportSelectSavedProfile(id){
 exportCapture();const d=nativeExportUI.draft,p=nativeExportUI.profiles.find(p=>p.id===id);
 if(!p){d.profile_id="";d.editProfileID="";d.savedProfileName="";exportRender();return;}
 if(p.target!==d.target){nativeExportUI.error="Choose a profile for this frontend.";exportRender();return;}
 Object.assign(d,{profile_id:p.id,editProfileID:p.id,savedProfileName:p.name,profile_name:p.name,profile_yaml:p.profile_yaml,profile_mode:"yaml"});
 delete d.policy;delete d.profileRaw;nativeExportUI.error="";exportEnsurePolicy();
}
async function exportSaveProfile(copy=false,remove=false){
 const d=nativeExportUI.draft;if(!d||nativeExportUI.busy)return;
 exportCapture();
 const id=copy?"":(d.editProfileID||d.profile_id||"");
 if(remove&&!window.confirm("Remove this saved profile? Exported files are kept."))return;
 nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
 try{
  const result=remove?null:await exportResolvePolicy(d);
  const name=(d.savedProfileName||"").trim();
  if(!remove&&!name)throw Error("Give this profile a name.");
  const data=await catalogPost("/api/export/profiles",{revision:nativeExportUI.profilesRevision,profile:remove?{id}:{id,name,target:d.target,profile_yaml:result.profile_yaml},delete:remove});
  nativeExportUI.profiles=data.profiles;nativeExportUI.profilesRevision=data.revision;
  if(remove){d.profile_id="";d.editProfileID="";d.savedProfileName="";d.profile_name="Custom profile";}
  else{
   const saved=data.profiles.find(p=>id?p.id===id:p.name===name);
   Object.assign(d,{profile_id:saved.id,editProfileID:saved.id,savedProfileName:saved.name,profile_name:saved.name,profile_yaml:saved.profile_yaml,profile_mode:"yaml"});
   exportInstallPolicy(d,result);
  }
 }catch(error){nativeExportUI.error=error.message;}
 finally{nativeExportUI.busy=false;exportRender();}
}
function exportSavedProfileControls(d){
 const id=d.editProfileID||d.profile_id||"";
 return '<div class="export-inline"><label>Saved profile<select id="export-saved-profile"><option value="">Unsaved profile</option>'+
 nativeExportUI.profiles.filter(p=>p.target===d.target).map(p=>'<option value="'+escapeAttr(p.id)+'" '+(id===p.id?'selected':'')+'>'+escapeHTML(p.name)+'</option>').join("")+
 '</select></label><label>Profile name<input id="export-saved-profile-name" placeholder="e.g. Original artwork · English" value="'+escapeAttr(d.savedProfileName??(id?d.profile_name:""))+'"></label></div>'+
 '<div class="export-profile-tools"><button type="button" class="tab-button" id="export-profile-save" '+(!d.policy?'disabled':'')+'>'+(id?'Update profile':'Save profile')+'</button>'+
 (id?'<button type="button" class="tab-button" id="export-profile-save-copy">Save as new</button><button type="button" class="tab-button" id="export-profile-remove">Remove profile</button>':'')+
 '<button type="button" class="tab-button" id="export-profile-reload">Reload saved</button></div>'+(id?'<small>Updating this profile also updates future exports from destinations that use it.</small>':'');
}

async function exportDownloadProfile(){
 const current=nativeExportUI.draft;if(!current||!current.policy||nativeExportUI.busy)return;
 exportCapture();const d=JSON.parse(JSON.stringify(current)),button=$("export-profile-download");button.disabled=true;
 try{
  const result=await exportResolvePolicy(d);
  const url=URL.createObjectURL(new Blob([result.profile_yaml],{type:"application/yaml"}));
  const link=document.createElement("a");link.href=url;link.download="rommeta-"+result.target+"-export.yaml";
  try{link.click();}finally{setTimeout(()=>URL.revokeObjectURL(url),0);}
 }catch(error){if(nativeExportUI.draft===current){nativeExportUI.error=error.message;exportRender();}}
 finally{button.disabled=false;}
}

function exportProfileEditor(d){
 if(d.profileLoading)return '<section class="export-profile-editor"><h2>Selection profile</h2><p class="secondary">Loading profile…</p></section>';
 const p=d.policy;
 const toolbar='<div class="register-heading"><h2>Selection profile</h2><button type="button" class="tab-button" data-export-preview '+(!p?'disabled':'')+'>Preview in new tab ↗</button></div>'+
 '<div class="export-profile-tools"><span id="export-profile-name" class="secondary">'+escapeHTML(d.profile_name||"Library settings")+'</span><button type="button" class="tab-button" id="export-profile-reset">Reset to library settings</button><button type="button" class="tab-button" id="export-profile-download" title="Download this profile as YAML for web or CLI export" '+(!p?'disabled':'')+'>Download YAML</button><label class="tab-button export-profile-upload">Load YAML<input id="export-profile-upload" type="file" accept=".yaml,.yml"></label></div>';
 if(!p)return '<section class="export-profile-editor">'+toolbar+'<p class="secondary">Load or reset a profile to configure it here.</p></section>';
 const supported=nativeExportUI.profileOptions?.media_roles?.[d.target]||[],choices=[...supported,...Object.keys(p.selection.media).filter(id=>!supported.some(role=>role.id===id)).map(id=>({id,label:id+" (check frontend support)"}))];
 const select=(key,label,value,options)=>'<label>'+label+'<select data-profile-field="'+key+'">'+options.map(([id,text])=>'<option value="'+id+'" '+(value===id?'selected':'')+'>'+text+'</option>').join("")+'</select></label>';
 const input=(key,label,value)=>'<label>'+label+'<input data-profile-field="'+key+'" value="'+escapeAttr((value||[]).join(", "))+'"></label>';
 const ruleKeys=["description",...Object.keys(p.selection.fields).map(k=>"fields."+k),...Object.keys(p.selection.media).map(k=>"media."+k)];
 if(!ruleKeys.includes(d.ruleKey))d.ruleKey="description";
 const label=key=>key==="description"?"Description":(key.startsWith("fields.")?"Metadata · ":"Media · ")+key.split(".")[1].replaceAll("_"," ");
 const rule=exportProfileRule(d,d.ruleKey),selected=rule.sources||[],available=[...new Set([...selected,...(nativeExportUI.profileOptions?.available_sources||[])])];
 const sources=available.map(source=>{const index=selected.indexOf(source);return '<div class="export-policy-source"><label><input type="checkbox" data-profile-source="'+escapeAttr(source)+'" '+(index>=0?'checked':'')+'>'+escapeHTML(sourceLabel(source))+'</label>'+(index>=0?'<span><button type="button" class="tab-button" data-profile-move="'+escapeAttr(source)+'" data-step="-1" '+(!index?'disabled':'')+' aria-label="Move '+escapeAttr(sourceLabel(source))+' up">↑</button><button type="button" class="tab-button" data-profile-move="'+escapeAttr(source)+'" data-step="1" '+(index===selected.length-1?'disabled':'')+' aria-label="Move '+escapeAttr(sourceLabel(source))+' down">↓</button></span>':'')+'</div>';}).join("");
 return '<section class="export-profile-editor">'+toolbar+exportSavedProfileControls(d)+'<div class="export-inline">'+
 select("titles.mode","Game titles",p.titles.mode,[["regional","Regional title"],["release","Full release name"]])+
 input("selection.description.languages","Description languages · first preferred",p.selection.description.languages)+
 select("selection.description.scope","Description from",p.selection.description.scope,[["game","Whole game"],["release","Selected release"]])+
 (d.target==="launchbox"?input("launchbox.regions","Preferred regions · first preferred",p.launchbox.regions):'')+
 '<label>Artwork<select id="export-profile-artwork">'+[["auto","Loaded profile priorities"],["original","Original images first"],["reconstructed","Reconstructed images first"]].map(([value,label])=>'<option value="'+value+'" '+(exportArtworkPreference(d)===value?'selected':'')+'>'+label+'</option>').join("")+'</select></label></div>'+
 '<p class="secondary">Artwork choices use saved image labels, with fallback when the preferred type is unavailable. Video and manuals keep their own rules.</p>'+
 '<fieldset class="export-policy-media"><legend>Media to include</legend><div class="export-policy-media-options">'+choices.map(role=>'<label><input type="checkbox" data-profile-media="'+escapeAttr(role.id)+'" '+(p.selection.media[role.id]?'checked':'')+'><span>'+escapeHTML(role.label||role.id)+'</span></label>').join("")+'</div></fieldset>'+
 '<details class="export-policy-priorities" '+(d.prioritiesOpen?'open':'')+'><summary>Source priorities & selection rules</summary><label>Choose field<select id="export-policy-rule">'+ruleKeys.map(key=>'<option value="'+key+'" '+(d.ruleKey===key?'selected':'')+'>'+escapeHTML(label(key))+'</option>').join("")+'</select></label><p class="secondary">Top source is preferred. Uncheck a source to exclude it for this field.</p>'+sources+
 select("rule.scope","Choose from",rule.scope,[["game","Whole game"],["release","Selected release"]])+
 (d.ruleKey==="description"?'<button type="button" class="tab-button" id="export-profile-copy-sources">Use this source order for all metadata fields</button>':'')+'</details>'+
 '<details class="export-policy-raw" '+(d.rawOpen?'open':'')+'><summary>Advanced profile YAML</summary><p class="secondary">Validate YAML to use it in this form. All profile changes are checked again before saving a destination or preparing a plan.</p><textarea id="export-policy-yaml" aria-label="Profile YAML" spellcheck="false">'+escapeHTML(d.profileRaw??d.profile_yaml??"")+'</textarea><button type="button" class="tab-button" id="export-profile-yaml-apply">Validate & use YAML</button> <button type="button" class="tab-button" id="export-profile-yaml-discard">Discard YAML edits</button><p class="secondary" id="export-profile-pending" '+(d.profileRaw===undefined?'hidden':'')+'>Validate or discard YAML edits before changing other settings or continuing.</p></details></section>';
}
function exportProfileRule(d,key){return key.split(".").reduce((obj,key)=>obj[key],d.policy.selection);}
function exportPolicyChanged(){
 const d=nativeExportUI.draft;d.editProfileID=d.editProfileID||d.profile_id||"";d.profile_id="";d.profile_mode="custom";d.profile_name="Unsaved changes";delete d.profileRaw;d.profile_yaml="";nativeExportUI.error="";if($("export-profile-name"))$("export-profile-name").textContent=d.profile_name;
}
async function exportResolvePolicy(d){
 if(d.profileRaw!==undefined)throw Error("Validate your YAML edits before continuing.");
 return catalogPost("/api/export/profile",d.policy?{target:d.target,profile:d.policy}:{target:d.target,profile_yaml:d.profile_yaml||""});
}
async function exportEnsurePolicy(){
 const d=nativeExportUI.draft;if(!d||d.policy||d.profileLoading)return;
 d.profileLoading=true;exportRender();
 try{
  if(d.profile_id){const saved=nativeExportUI.profiles.find(p=>p.id===d.profile_id);if(!saved)throw Error("Saved profile no longer exists. Choose another profile or reset to library settings.");if(saved.target!==d.target)throw Error("Saved profile uses a different frontend.");d.profile_yaml=saved.profile_yaml;d.profile_name=saved.name;d.savedProfileName=saved.name;d.editProfileID=saved.id;}
  const result=await exportResolvePolicy(d);
  if(nativeExportUI.draft!==d)return;
  exportInstallPolicy(d,result);
  if(!nativeExportUI.profileOptions)nativeExportUI.profileOptions=await fetchJSON("/api/export-options");
 }catch(error){if(nativeExportUI.draft===d)nativeExportUI.error=error.message;}
 finally{d.profileLoading=false;if(nativeExportUI.draft===d&&state.view==="export")exportRender();}
}
async function exportPreviewProfile(){
 exportCapture();
 if(!catalogUI.enabled){setView("browse");setExportMode(true);return;}
 const current=nativeExportUI.draft;if(!current||nativeExportUI.busy)return;const d=JSON.parse(JSON.stringify(current));
 // Reserve the tab in the click handler so async validation does not trigger a popup block.
 const tab=window.open("about:blank","_blank");
 if(!tab){nativeExportUI.error="Allow a new tab for the export preview.";exportRender();return;}
 try{
  const result=await exportResolvePolicy(d);
  tab.sessionStorage.setItem("rommeta.export-preview",JSON.stringify({profile_yaml:result.profile_yaml,target:d.target,platform:d.all?"":d.platform,name:d.profile_name||"Export profile"}));
  tab.opener=null;tab.location.replace("/?export-preview=1");
 }catch(error){tab.close();nativeExportUI.error=error.message;if(state.view==="export")exportRender();}
}
function exportProfilePending(){
 const pending=nativeExportUI.draft?.profileRaw!==undefined;
 document.querySelectorAll("[data-profile-field], [data-profile-media], [data-profile-source], [data-profile-move], #export-profile-copy-sources, #export-profile-artwork, #export-profile-download, #export-profile-save, #export-profile-save-copy, #export-save-destination, #export-run-form button[type=submit], [data-export-preview]").forEach(el=>{if(pending){if(el.profileDisabled===undefined)el.profileDisabled=el.disabled;el.disabled=true;}else if(el.profileDisabled!==undefined){el.disabled=el.profileDisabled;delete el.profileDisabled;}});
 if($("export-profile-pending"))$("export-profile-pending").hidden=!pending;
}
function exportBindProfile(){
 const d=nativeExportUI.draft;if(!d)return;
 exportProfilePending();
 $("export-profile-artwork")?.addEventListener("change",e=>exportChangeArtwork(e.target.value));
 $("export-profile-download")?.addEventListener("click",exportDownloadProfile);
 $("export-saved-profile")?.addEventListener("change",e=>exportSelectSavedProfile(e.target.value));
 $("export-profile-save")?.addEventListener("click",()=>exportSaveProfile());
 $("export-profile-save-copy")?.addEventListener("click",()=>exportSaveProfile(true));
 $("export-profile-remove")?.addEventListener("click",()=>exportSaveProfile(false,true));
 $("export-profile-reload")?.addEventListener("click",async()=>{exportCapture();try{await exportLoadProfiles();nativeExportUI.error="";}catch(error){nativeExportUI.error=error.message;}exportRender();});
 $("export-profile-yaml-discard")?.addEventListener("click",()=>{delete d.profileRaw;d.rawOpen=false;nativeExportUI.error="";exportRender();});
 $("export-profile-reset")?.addEventListener("click",()=>{exportCapture();d.profile_id="";d.editProfileID="";d.savedProfileName="";delete d.policy;delete d.profileRaw;d.profile_yaml="";d.profile_mode="defaults";d.profile_name="Library settings";nativeExportUI.error="";exportEnsurePolicy();});
 document.querySelectorAll("[data-profile-field]").forEach(el=>el.addEventListener(el.tagName==="SELECT"?"change":"input",()=>{
  const key=el.dataset.profileField,path=key.split("."),field=path.pop(),obj=path[0]==="rule"?exportProfileRule(d,d.ruleKey):path.reduce((obj,k)=>obj[k],d.policy);
  obj[field]=["languages","regions"].includes(field)?el.value.split(",").map(s=>s.trim()).filter(Boolean):el.value;
  exportPolicyChanged();
 }));
 document.querySelectorAll("[data-profile-media]").forEach(el=>el.addEventListener("change",()=>{
  const key=el.dataset.profileMedia,preference=exportArtworkPreference(d);d.disabledMedia=d.disabledMedia||{};
  if(el.checked)d.policy.selection.media[key]=d.disabledMedia[key]||{scope:"release",order:["region","source","format"],sources:[...(nativeExportUI.profileOptions?.[key==="video"?"video_sources":"artwork_sources"]||[])],...(key==="manual"?{formats:["pdf"]}:{})};
  else{d.disabledMedia[key]=d.policy.selection.media[key];delete d.policy.selection.media[key];}
  if(el.checked)exportApplyArtwork(d,key,d.policy.selection.media[key],preference);
  exportPolicyChanged();exportRender();
 }));
 $("export-policy-rule")?.addEventListener("change",e=>{d.ruleKey=e.target.value;exportRender();});
 document.querySelectorAll("[data-profile-source]").forEach(el=>el.addEventListener("change",()=>{
  const rule=exportProfileRule(d,d.ruleKey),source=el.dataset.profileSource;rule.sources=el.checked?[...(rule.sources||[]),source]:(rule.sources||[]).filter(x=>x!==source);
  exportPolicyChanged();exportRender();
 }));
 document.querySelectorAll("[data-profile-move]").forEach(el=>el.addEventListener("click",()=>{
  const list=exportProfileRule(d,d.ruleKey).sources,i=list.indexOf(el.dataset.profileMove),j=i+Number(el.dataset.step);
  if(i<0||j<0||j>=list.length)return;[list[i],list[j]]=[list[j],list[i]];exportPolicyChanged();exportRender();
 }));
 $("export-profile-copy-sources")?.addEventListener("click",()=>{for(const rule of Object.values(d.policy.selection.fields))rule.sources=[...d.policy.selection.description.sources];exportPolicyChanged();exportRender();});
 document.querySelector?.(".export-policy-priorities")?.addEventListener("toggle",e=>{d.prioritiesOpen=e.target.open;});
 document.querySelector?.(".export-policy-raw")?.addEventListener("toggle",async e=>{
  d.rawOpen=e.target.open;if(!d.rawOpen||d.profileRaw!==undefined)return;
  try{const signature=JSON.stringify(d.policy),result=await exportResolvePolicy(d);if(nativeExportUI.draft===d&&d.rawOpen&&d.profileRaw===undefined&&JSON.stringify(d.policy)===signature){d.profile_yaml=result.profile_yaml;$("export-policy-yaml").value=result.profile_yaml;}}
  catch(error){d.rawOpen=false;nativeExportUI.error=error.message;exportRender();}
 });
 $("export-policy-yaml")?.addEventListener("input",e=>{d.profileRaw=e.target.value;exportProfilePending();});
 $("export-profile-yaml-apply")?.addEventListener("click",async()=>{
  const raw=d.profileRaw??$("export-policy-yaml").value;
  try{const result=await catalogPost("/api/export/profile",{target:d.target,profile_yaml:raw});if(nativeExportUI.draft!==d||d.profileRaw!==undefined&&d.profileRaw!==raw)return;exportInstallPolicy(d,result);delete d.profileRaw;d.editProfileID=d.editProfileID||d.profile_id||"";d.profile_id="";d.profile_mode="custom";d.profile_name="Unsaved changes";nativeExportUI.error="";exportRender();}
  catch(error){if(nativeExportUI.draft===d){nativeExportUI.error=error.message;exportRender();}}
 });
}


async function exportUseBrowserProfile(){
 try{const result=await requestPreview([]);if(!nativeExportUI.draft)nativeExportUI.draft=exportDraft();exportInstallPolicy(nativeExportUI.draft,result);Object.assign(nativeExportUI.draft,{target:result.target,profile_mode:"custom",profile_name:"Custom profile"});libraryNavigate("export");}catch(error){$("export-error").textContent=error.message;$("export-error").classList.remove("hidden");}
}
