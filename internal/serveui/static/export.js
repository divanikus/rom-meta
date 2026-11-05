/* Native export on the serve host. Plans and progress are owned by Operations. */
const nativeExportUI={loaded:false,loading:false,busy:false,error:"",revision:"",destinations:[],host:"",os:"",draft:null,folder:null,folderVersion:0};
const exportStates={planning:"Preparing export plan",ready:"Ready to export",needs_review:"Conflicts to resolve",exporting:"Exporting",succeeded:"Complete",incomplete:"Stopped after partial write",failed:"Could not finish"};
function exportStageLabel(stage){const labels={catalog:"Opening library",checking:"Checking reviewed inputs",starting:"Starting",planned:"Plan ready",complete:"Complete",write:"Writing files","native fields":"Preparing frontend metadata","read destination":"Reading destination"};return labels[stage]||((stage||"Starting").replace(/^./,s=>s.toUpperCase()));}
function exportDraft(){return {id:"",name:"",target:state.exportSettings?.target||"es-de",path:"",rom_root:"",media_root:"",platform:state.platform||"",all:false,profile_yaml:"",profile_name:"Library defaults",profile_mode:"defaults"}}
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
 pane.innerHTML='<div class="management-heading"><div><span class="operation-eyebrow">EXPORT TO A FRONTEND</span><h1>Export</h1><p>Write metadata, playlists and media for your existing ROMs.</p></div><button class="tab-button" data-export-preview>Build profile visually</button></div>'+
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
 '<label>Selection profile<select id="export-profile-mode"><option value="defaults" '+(f.profile_mode==="defaults"?'selected':'')+'>Library defaults</option><option value="current" '+(f.profile_mode==="current"?'selected':'')+'>Current preview settings</option><option value="yaml" '+(f.profile_mode==="yaml"?'selected':'')+'>YAML profile</option></select></label>'+
 (f.profile_mode==="yaml"?'<label>Choose profile<input id="export-profile-upload" type="file" accept=".yaml,.yml"></label><p class="secondary">'+escapeHTML(f.profile_name||"Choose a YAML profile")+'</p>'+(f.profile_yaml?'<details><summary>Profile contents</summary><pre class="export-profile-text">'+escapeHTML(f.profile_yaml)+'</pre></details>':""):f.profile_mode==="current"?'<p class="secondary">Uses the languages, source priorities and media choices from Build profile visually.</p>':'<p class="secondary">Uses the library’s saved source priorities and default export policy.</p>')+
 '</section></div><div class="export-prepare-bar"><button class="catalog-primary" type="submit">'+(nativeExportUI.busy?'Preparing…':'Prepare export plan')+'</button><p>Scan ROMs and review the proposed changes before applying. Progress stays in Operations after you close the browser.</p></div></fieldset></form>';
 exportBind();
}
async function exportDestinationInput(){
 exportCapture();const f=nativeExportUI.draft,d={...f};delete d.profile_mode;
 if(f.profile_mode==="defaults"){d.profile_yaml="";d.profile_name="Library defaults";}
 if(f.profile_mode==="current"){const p=await requestPreview([],undefined,{...state.exportSettings,target:d.target});d.profile_yaml=p.profile_yaml;d.profile_name="Preview settings";}
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
 document.querySelectorAll("[data-export-preview]").forEach(b=>b.addEventListener("click",exportPreviewProfile));
 $("export-run-form")?.addEventListener("submit",exportPrepare);
 $("export-run-form")?.addEventListener("input",exportCapture);
 for(const id of ["export-run-target","export-rom-mode","export-profile-mode"])$(id)?.addEventListener("change",()=>{exportCapture();exportRender();});
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
 '<details '+(review?'open':'')+'><summary>Reviewed games ('+n.games+')</summary>'+n.changes.map(c=>'<div class="export-change"><strong>'+escapeHTML(c.title)+'</strong><span>'+escapeHTML(c.platform)+' · '+escapeHTML(c.action)+'</span><small>'+escapeHTML((c.filled_fields||[]).join(", "))+' · '+c.added_versions+' versions · '+c.added_media+' media · '+c.added_playlists+' playlists</small></div>').join("")+(n.games>n.changes.length?'<p>Showing the first '+n.changes.length+' games. Download the full plan for every change.</p>':"")+'</details>':"")+
 (review?'<p>The destination is unchanged. Apply writes the reviewed files using the normal additive export rules. Close the frontend before applying.</p>':job.state==="succeeded"?'<p>'+(x.written?"Export completed.":"The destination is already up to date; no files were written.")+'</p>':job.state==="incomplete"?'<p>Some files were written before the error. Prepare another plan against the current destination.</p>':"")+
 '<div class="operation-actions">'+(job.state==="ready"?'<button class="catalog-primary" data-native-export-apply="'+job.id+'">'+(n?.files?"Apply export":"Finish — already up to date")+'</button>':"")+
 (review?'<a class="tab-button" href="/api/export/plan?id='+encodeURIComponent(job.id)+'" download>Download full plan</a>':"")+
 (!busy?'<button class="tab-button" data-native-export-again>Prepare another plan</button><button class="tab-button" data-import-dismiss="'+job.id+'">Dismiss</button>':"")+'</div></article>';
}
function nativeExportBindOperation(job){
 if(job?.kind!=="export")return;
 document.querySelectorAll("[data-native-export-apply]").forEach(b=>b.addEventListener("click",async()=>{b.disabled=true;await libraryImportAction("/api/export/apply",{id:job.id,revision:job.revision});}));
 document.querySelectorAll("[data-native-export-again]").forEach(b=>b.addEventListener("click",()=>{nativeExportUI.draft={...job.export.destination,profile_mode:"yaml"};nativeExportUI.error="";libraryNavigate("export");}));
}
