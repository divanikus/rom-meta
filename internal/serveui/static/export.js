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
 '<div class="operation-actions"><button type="button" class="tab-button" id="export-save-destination">Save destination</button>'+(f.id?'<button type="button" class="tab-button" id="export-delete-destination">Remove saved destination</button>':"")+'<button type="button" class="tab-button" id="export-reload-destinations">Reload saved</button></div></section>'+
 '<section class="operation-detail"><h2>ROMs and profile</h2>'+exportPathField("ROM folder","export-rom-root","rom_root",f.all?"A parent folder containing platform folders, for example nes, snes and psx.":"The folder containing ROMs for this platform.")+
 '<div class="export-inline"><label>Folder layout<select id="export-rom-mode"><option value="single" '+(!f.all?'selected':'')+'>One platform</option><option value="all" '+(f.all?'selected':'')+'>All platform folders</option></select></label>'+
 (!f.all?'<label>Platform<select id="export-run-platform"><option value="">Choose platform</option>'+platforms.map(p=>'<option value="'+escapeAttr(p.platform)+'" '+(p.platform===f.platform?'selected':'')+'>'+escapeHTML(p.platform)+'</option>').join("")+'</select></label>':"")+'</div>'+
 exportProfileEditor(f)+'</section></div><div class="export-prepare-bar"><button class="catalog-primary" type="submit" '+(!f.policy||f.profileRaw!==undefined?'disabled':'')+'>'+(nativeExportUI.busy?'Preparing…':'Prepare export plan')+'</button><p>Review the plan before applying.</p></div></fieldset></form>';
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
function exportPrimaryAction(job){
 if(!["ready","needs_review"].includes(job.state))return "";
 const pending=exportSuggestionsPending(job);
 if(exportReviewDirty(job)||pending)return '<button class="catalog-primary" data-export-review-update '+(pending?'disabled':'')+'>'+(pending?'Accepting matches…':'Update export plan')+'</button>';
 return job.state==="ready"?'<button class="catalog-primary" data-native-export-apply="'+job.id+'">'+(job.export.totals?.files?'Apply export':'Finish — already up to date')+'</button>':"";
}
function nativeExportDetail(job){
 const x=job.export,d=x.destination,n=x.totals,p=x.progress||{},busy=operationBusy(job),review=["ready","needs_review"].includes(job.state);
 return '<article class="operation-detail">'+operationDismiss(job,"",true)+'<div class="operation-eyebrow">FRONTEND EXPORT · '+operationStatus(job.state,exportStates[job.state]||job.state)+'</div><h2>'+escapeHTML(d.name)+'</h2>'+
 '<dl class="operation-facts"><dt>Frontend</dt><dd>'+escapeHTML(d.target)+'</dd><dt>Destination</dt><dd>'+escapeHTML(d.path)+'</dd><dt>ROMs</dt><dd>'+escapeHTML(d.rom_root)+'</dd><dt>Scope</dt><dd>'+escapeHTML(d.all?"All platform folders":d.platform)+'</dd><dt>Profile</dt><dd>'+escapeHTML(d.profile_name)+'</dd>'+(d.media_root?'<dt>Media</dt><dd>'+escapeHTML(d.media_root)+'</dd>':"")+'</dl>'+
 (busy?'<div class="operation-progress" role="status"><strong>'+escapeHTML(exportStageLabel(p.Stage))+'</strong><span>'+escapeHTML(p.Item||"")+'</span>'+operationProgressMeter(p.Current,p.Total,exportProgressUnit(p.Stage))+'</div>':"")+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+
 (n?'<div class="operation-summary"><div><strong>'+formatNumber(n.matched)+'</strong><span>matched ROM entries</span></div><div><strong>'+formatNumber(n.files)+'</strong><span>files in the plan</span></div><div><strong>'+formatNumber(x.written)+'</strong><span>files written</span></div></div><p class="secondary">'+formatNumber(n.unmatched)+' unmatched · '+formatNumber(n.ambiguous)+' ambiguous · '+formatNumber(n.unsupported)+' unsupported</p>'+
 (n.conflicts?.length?'<div class="operation-error"><strong>Resolve conflicts and prepare a new plan</strong><ul>'+n.conflicts.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></div>':"")+
 (n.warnings?.length?'<details><summary>Warnings ('+n.warnings.length+')</summary><ul>'+n.warnings.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></details>':"")+
 (n.skipped?.length?'<details><summary>Skipped platforms ('+n.skipped.length+')</summary><ul>'+n.skipped.map(v=>'<li>'+escapeHTML(v.system)+': '+escapeHTML(v.reason)+'</li>').join("")+'</ul></details>':"")+
 exportIssues(job):"")+
 (review&&!exportReviewDirty(job)&&!exportSuggestionsPending(job)?'<p>Close the frontend before applying.</p>':job.state==="succeeded"?'<p>'+(x.written?"Export completed.":"The destination is already up to date; no files were written.")+'</p>':job.state==="incomplete"?'<p>Some files were written before the error. Prepare another plan against the current destination.</p>':"")+
 (review&&exportReviewDirty(job)?'<p id="export-review-hint" class="secondary">'+exportReviewHint(job)+'</p>':"")+
 '<div class="operation-actions">'+exportPrimaryAction(job)+
 (review?'<a class="tab-button" href="/api/export/plan?id='+encodeURIComponent(job.id)+'" download>Download full plan</a>':"")+
 (!busy?'<button class="tab-button" data-native-export-again>'+(["failed","incomplete"].includes(job.state)?"Retry — review a new plan":"Prepare another plan")+'</button>'+operationDismiss(job):"")+'</div></article>';
}
function nativeExportBindOperation(job){
 if(job?.kind!=="export")return;
 exportBindReview(job);
 document.querySelectorAll("[data-native-export-apply]").forEach(b=>b.addEventListener("click",async()=>{if(exportReviewDirty(job)||exportSuggestionsPending(job))return;b.disabled=true;await libraryImportAction("/api/export/apply",{id:job.id,revision:job.revision});}));
 document.querySelectorAll("[data-native-export-again]").forEach(b=>b.addEventListener("click",()=>{nativeExportUI.draft={...job.export.destination,profile_mode:"yaml"};nativeExportUI.error="";libraryNavigate("export");}));
}
