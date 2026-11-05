/* Native export on the serve host. Plans and progress are owned by Operations. */
const nativeExportUI={reviewDrafts:new Map(),loaded:false,loading:false,busy:false,error:"",revision:"",destinations:[],host:"",os:"",draft:null,folder:null,folderVersion:0};
const exportStates={planning:"Preparing export plan",ready:"Ready to export",needs_review:"Conflicts to resolve",exporting:"Exporting",succeeded:"Complete",incomplete:"Stopped after partial write",failed:"Could not finish"};
function exportStageLabel(stage){const labels={catalog:"Opening library",checking:"Checking reviewed inputs",starting:"Starting",planned:"Plan ready",complete:"Complete",write:"Writing files","native fields":"Preparing frontend metadata","read destination":"Reading destination"};return labels[stage]||((stage||"Starting").replace(/^./,s=>s.toUpperCase()));}
function exportDraft(){return {id:"",name:"",target:state.exportSettings?.target||"es-de",path:"",rom_root:"",media_root:"",platform:state.platform||"",all:false,profile_yaml:"",profile_name:"Library settings",profile_mode:"defaults"}}
async function exportOpen(){
 if(!nativeExportUI.draft)nativeExportUI.draft=exportDraft();
 exportRender();
 if(nativeExportUI.loaded||nativeExportUI.loading)return;
 await exportLoadDestinations();
}
async function exportLoadDestinations(){
 nativeExportUI.loading=true;nativeExportUI.error="";exportRender();
 try{const data=await fetchJSON("/api/export/destinations");Object.assign(nativeExportUI,data,{loaded:true});}
 catch(error){nativeExportUI.error=error.message;}
 finally{nativeExportUI.loading=false;if(state.view==="export")exportRender();}
}
function exportCapture(){
 const f=nativeExportUI.draft;if(!f||!$("export-run-form"))return;
 for(const [key,id]of Object.entries({name:"export-destination-name",target:"export-run-target",path:"export-destination-path",rom_root:"export-rom-root",platform:"export-run-platform",media_root:"export-media-root",profile_mode:"export-profile-mode"})){const el=$(id);if(el)f[key]=el.value;}
 f.all=$("export-rom-mode")?.value==="all";
}
function exportPathField(label,id,key,hint){
 const value=nativeExportUI.draft[key]||"";
 return '<label>'+label+'<span class="export-path-control"><input id="'+id+'" value="'+escapeAttr(value)+'" placeholder="Absolute path on the server"><button type="button" class="tab-button" data-export-browse="'+key+'">Browse…</button></span>'+(hint?'<small>'+hint+'</small>':"")+'</label>';
}
function exportRender(){
 const pane=$("native-export-pane");if(!pane)return;
 if(!catalogUI.enabled){pane.innerHTML='<h1>Export</h1><p>Start serve with --edit to save destinations and run exports on this machine.</p><button class="tab-button" data-export-preview>Preview profile</button>';exportBind();return;}
 const f=nativeExportUI.draft||exportDraft(),busy=nativeExportUI.busy||nativeExportUI.loading;
 const platforms=state.platforms||[];
 pane.innerHTML='<div class="management-heading"><div><span class="operation-eyebrow">EXPORT TO A FRONTEND</span><h1>Export</h1><p>Write metadata, playlists and media for your existing ROMs.</p></div><button class="tab-button" data-export-preview>Edit browser profile</button></div>'+
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
 '<label>Selection profile<select id="export-profile-mode"><option value="defaults" '+(f.profile_mode==="defaults"?'selected':'')+'>Library settings (saved priorities)</option><option value="current" '+(f.profile_mode==="current"?'selected':'')+'>Profile edited in this browser</option><option value="yaml" '+(f.profile_mode==="yaml"?'selected':'')+'>YAML profile</option></select></label>'+
 (f.profile_mode==="yaml"?'<label>Choose profile<input id="export-profile-upload" type="file" accept=".yaml,.yml"></label><p class="secondary">'+escapeHTML(f.profile_name||"Choose a YAML profile")+'</p>'+(f.profile_yaml?'<details><summary>Profile contents</summary><pre class="export-profile-text">'+escapeHTML(f.profile_yaml)+'</pre></details>':""):f.profile_mode==="current"?'<p class="secondary">Uses the title rules, languages, source order and media choices edited in this browser. Open “Edit this profile” to change them.</p>':'<p class="secondary">Uses source priorities saved in Settings, with the default title, language and media rules. “View effective profile” shows the exact resolved policy.</p>')+
 '<div class="export-profile-summary"><button type="button" class="tab-button" data-export-inspect>View effective profile</button>'+(f.profile_mode==="current"?'<button type="button" class="tab-button" data-export-preview>Edit this profile</button>':'')+(f.inspected_profile?exportPolicySummary(f.inspected_policy)+'<details><summary>Full profile YAML</summary><pre class="export-profile-text">'+escapeHTML(f.inspected_profile)+'</pre></details>':'')+'</div></section></div><div class="export-prepare-bar"><button class="catalog-primary" type="submit">'+(nativeExportUI.busy?'Preparing…':'Prepare export plan')+'</button><p>Scan ROMs and review the proposed changes before applying. Progress stays in Operations after you close the browser.</p></div></fieldset></form>';
 exportBind();
}
async function exportDestinationInput(){
 exportCapture();const f=nativeExportUI.draft,d=Object.fromEntries(["id","name","target","path","rom_root","media_root","platform","all","profile_yaml","profile_name"].map(k=>[k,f[k]]));
 if(f.profile_mode==="defaults"){d.profile_yaml="";d.profile_name="Library settings";}
 if(f.profile_mode==="current"){const p=await requestPreview([],undefined,{...state.exportSettings,target:d.target});d.profile_yaml=p.profile_yaml;d.profile_name="Browser profile";}
 if(f.profile_mode==="yaml"&&!d.profile_yaml)throw Error("Choose a YAML profile first.");
 if(d.all)d.platform="";
 if(d.target!=="es-de")d.media_root="";
 return d;
}
async function exportSaveDestination(remove=false){
 if(nativeExportUI.busy)return;
 exportCapture();nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
 try{
  const d=remove?{id:nativeExportUI.draft.id}:await exportDestinationInput();
  if(remove&&!window.confirm("Remove this saved destination? Files in the destination are kept."))return;
  nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
  const data=await catalogPost("/api/export/destinations",{revision:nativeExportUI.revision,destination:d,delete:remove});
  Object.assign(nativeExportUI,data);
  if(remove)nativeExportUI.draft=exportDraft();
  else{const saved=data.destinations.find(x=>x.id===d.id||x.name===d.name);nativeExportUI.draft={...saved,profile_mode:saved.profile_yaml?"yaml":"defaults"};}
 }catch(error){nativeExportUI.error=error.message;}
 finally{nativeExportUI.busy=false;exportRender();}
}
async function exportPrepare(event){
 event?.preventDefault();if(nativeExportUI.busy)return;
 exportCapture();nativeExportUI.busy=true;nativeExportUI.error="";exportRender();
 try{
  const d=await exportDestinationInput();exportRender();
  const op=await catalogPost("/api/export/prepare",{destination:d});
  libraryRememberJob(op);libraryNavigate("operations");libraryPollOperations();
 }catch(error){nativeExportUI.error=error.message;exportRender();}
 finally{nativeExportUI.busy=false;if(state.view==="export")exportRender();}
}
function exportPreviewProfile(){
 exportCapture();
 if(nativeExportUI.draft){nativeExportUI.draft.profile_mode="current";if(state.exportSettings){state.exportSettings.target=nativeExportUI.draft.target;$("export-target").value=nativeExportUI.draft.target;}}
 setView("browse");setExportMode(true);
}
function exportBind(){
 document.querySelectorAll("[data-export-inspect]").forEach(b=>b.addEventListener("click",exportInspectProfile));
 document.querySelectorAll("[data-export-preview]").forEach(b=>b.addEventListener("click",exportPreviewProfile));
 $("export-run-form")?.addEventListener("submit",exportPrepare);
 $("export-run-form")?.addEventListener("input",exportCapture);
 for(const id of ["export-run-target","export-rom-mode","export-profile-mode"])$(id)?.addEventListener("change",()=>{exportCapture();delete nativeExportUI.draft.inspected_profile;delete nativeExportUI.draft.inspected_policy;exportRender();});
 $("export-destination-select")?.addEventListener("change",event=>{
  const saved=nativeExportUI.destinations.find(d=>d.id===event.target.value);
  nativeExportUI.draft=saved?{...saved,profile_mode:saved.profile_yaml?"yaml":"defaults"}:exportDraft();nativeExportUI.error="";exportRender();
 });
 $("export-save-destination")?.addEventListener("click",()=>exportSaveDestination());
 $("export-delete-destination")?.addEventListener("click",()=>exportSaveDestination(true));
 $("export-reload-destinations")?.addEventListener("click",()=>{exportCapture();exportLoadDestinations();});
 $("export-profile-upload")?.addEventListener("change",async event=>{
  const file=event.target.files?.[0];if(!file)return;
  const draft=nativeExportUI.draft;
  try{if(file.size>1048576)throw Error("Profile must be at most 1 MiB.");const content=await file.text();if(nativeExportUI.draft!==draft)return;draft.profile_yaml=content;draft.profile_name=file.name;nativeExportUI.error="";}
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
 return '<article class="operation-detail"><div class="operation-eyebrow">FRONTEND EXPORT · '+escapeHTML(exportStates[job.state]||job.state)+'</div><h2>'+escapeHTML(d.name)+'</h2>'+
 '<dl class="operation-facts"><dt>Frontend</dt><dd>'+escapeHTML(d.target)+'</dd><dt>Destination</dt><dd>'+escapeHTML(d.path)+'</dd><dt>ROMs</dt><dd>'+escapeHTML(d.rom_root)+'</dd><dt>Scope</dt><dd>'+escapeHTML(d.all?"All platform folders":d.platform)+'</dd><dt>Profile</dt><dd>'+escapeHTML(d.profile_name)+'</dd>'+(d.media_root?'<dt>Media</dt><dd>'+escapeHTML(d.media_root)+'</dd>':"")+'</dl>'+
 (busy?'<div class="operation-progress" role="status"><strong>'+escapeHTML(exportStageLabel(p.Stage))+'</strong><span>'+escapeHTML(p.Item||"")+'</span>'+(p.Total>0?'<progress value="'+Math.min(p.Total,Math.max(0,p.Current||0))+'" max="'+p.Total+'"></progress><span>'+formatNumber(p.Current)+' / '+formatNumber(p.Total)+'</span>':'<progress aria-label="Export in progress"></progress>')+'<p>You can close the browser and return to this operation.</p></div>':"")+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+
 (n?'<div class="operation-summary"><div><strong>'+formatNumber(n.matched)+'</strong><span>matched ROM entries</span></div><div><strong>'+formatNumber(n.files)+'</strong><span>files in the plan</span></div><div><strong>'+formatNumber(x.written)+'</strong><span>files written</span></div></div><p class="secondary">'+formatNumber(n.unmatched)+' unmatched · '+formatNumber(n.ambiguous)+' ambiguous · '+formatNumber(n.unsupported)+' unsupported</p>'+
 (n.conflicts?.length?'<div class="operation-error"><strong>Resolve conflicts and prepare a new plan</strong><ul>'+n.conflicts.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></div>':"")+
 (n.warnings?.length?'<details><summary>Warnings ('+n.warnings.length+')</summary><ul>'+n.warnings.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></details>':"")+
 (n.skipped?.length?'<details><summary>Skipped platforms ('+n.skipped.length+')</summary><ul>'+n.skipped.map(v=>'<li>'+escapeHTML(v.system)+': '+escapeHTML(v.reason)+'</li>').join("")+'</ul></details>':"")+
 exportIssues(job):"")+
 (review?'<p>The destination is unchanged. Apply writes the reviewed files using the normal additive export rules. Close the frontend before applying.</p>':job.state==="succeeded"?'<p>'+(x.written?"Export completed.":"The destination is already up to date; no files were written.")+'</p>':job.state==="incomplete"?'<p>Some files were written before the error. Prepare another plan against the current destination.</p>':"")+
 '<div class="operation-actions">'+(job.state==="ready"?'<button class="catalog-primary" data-native-export-apply="'+job.id+'">'+(n?.files?"Apply export":"Finish — already up to date")+'</button>':"")+
 (review?'<a class="tab-button" href="/api/export/plan?id='+encodeURIComponent(job.id)+'" download>Download full plan</a>':"")+
 (!busy?'<button class="tab-button" data-native-export-again>Prepare another plan</button><button class="tab-button" data-import-dismiss="'+job.id+'">Dismiss</button>':"")+'</div></article>';
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
async function exportInspectProfile(){
 exportCapture();const d=nativeExportUI.draft;
 try{
  const result=d.profile_mode==="defaults"?await fetchJSON("/api/export/profile?"+new URLSearchParams({target:d.target})):d.profile_mode==="current"?await requestPreview([],undefined,{...state.exportSettings,target:d.target}):{profile_yaml:d.profile_yaml};
  if(nativeExportUI.draft!==d)return;
  d.inspected_profile=result.profile_yaml||"Choose a YAML profile first.";d.inspected_policy=result.profile;nativeExportUI.error="";exportRender();
 }catch(error){nativeExportUI.error=error.message;exportRender();}
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


function exportPolicySummary(p){
 if(!p)return "";
 const s=p.selection||{},sources=list=>(list||[]).map(sourceLabel).join(" → ")||"None selected";
 const rows=[["Titles",p.titles?.mode==="release"?"Full release name":"Regional title"],["Descriptions",(s.description?.languages||[]).join(" → ")],["Metadata",sources(s.fields?.title?.sources)],["Artwork",sources(s.media?.cover?.sources)],["Video",sources(s.media?.video?.sources)],["Media types",Object.keys(s.media||{}).join(", ")||"None"]];
 return '<dl class="operation-facts">'+rows.map(([k,v])=>'<dt>'+escapeHTML(k)+'</dt><dd>'+escapeHTML(v)+'</dd>').join("")+'</dl>';
}
