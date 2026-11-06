/* Library navigation and server-owned library operations. No browser job queue. */
const libraryUI={jobs:[],selected:null,view:"library",timer:null,polling:false,uploading:false,uploadProgress:null,error:"",refreshed:new Set(),settings:null,file:null,drafts:new Map()};
const importStages={opening:"Opening DAT",parsing:"Reading releases",planning:"Preparing changes",bundling:"Grouping releases",writing:"Saving cards",importing:"Starting import"};
const operationLabels={needs_selection:"Choose a DAT",planning:"Preparing plan",ready:"Ready to import",needs_review:"Needs attention",importing:"Importing",succeeded:"Complete",failed:"Could not finish"};
function libraryNavigate(view){
 if(typeof catalogLeaveInline==="function"&&!catalogLeaveInline())return;
 libraryUI.view=view;
 if(view==="library"){setView("browse");setExportMode(false);}
 else setView(view);
 librarySyncNavigation();
 if(view==="operations"){libraryRenderOperations();libraryPollOperations();}
 if(view==="settings")libraryLoadSettings();
 if(view==="export"&&typeof exportOpen==="function")exportOpen();
}
function librarySyncNavigation(){
 if(typeof libraryUI==="undefined")return;
 const view=state.view==="browse"?(state.exportMode?"export":"library"):state.view,navView=view==="export"?"operations":view;
 libraryUI.view=view;
 $("app")?.classList.toggle("export-preview",state.exportMode&&state.view==="browse");
 $("top-status")?.classList.toggle("hidden",state.view!=="browse");
 for(const [id,target] of [["browse-tab","library"],["operations-tab","operations"],["settings-tab","settings"]]){
  $(id)?.classList.toggle("active",navView===target);$(id)?.setAttribute("aria-current",navView===target?"page":"false");
 }
 $("operations-pane")?.classList.toggle("hidden",view!=="operations");
 $("native-export-pane")?.classList.toggle("hidden",state.view!=="export");
 $("settings-pane")?.classList.toggle("hidden",view!=="settings");
 const empty=state.platformsLoaded&&state.platforms.length===0&&state.view==="browse";
 $("library-empty")?.classList.toggle("hidden",!empty);
 if(empty)for(const id of ["platform-pane","game-pane","detail-pane"])$(id).classList.add("hidden");
 const enabled=typeof catalogUI!=="undefined"&&catalogUI.enabled;
 librarySyncPlatformSelection();
 $("library-import")?.classList.toggle("hidden",!enabled);
 document.querySelectorAll("[data-import-new]").forEach(el=>el.classList.toggle("hidden",!enabled));
 $("empty-readonly")?.classList.toggle("hidden",enabled);
 $("card-tools")?.classList.toggle("hidden",!enabled||!state.game||view!=="library");

}
function operationTitle(job){if(job.kind==="export")return "Export "+job.export.destination.name;return job.kind==="enrichment"?"Fetch metadata · "+(job.enrichment.platforms?.length>1?job.enrichment.platforms.length+" platforms":job.platform):job.filename}
function operationLabel(job){if(job.kind==="export")return exportStates[job.state]||job.state;return job.kind==="enrichment"?(enrichmentStates[job.state]||job.state):(operationLabels[job.state]||job.state)}
function operationStatus(state,label){
 const tone=({succeeded:"success",running:"active",planning:"active",importing:"active",enriching:"active",exporting:"active",queued:"queued",ready:"ready",incomplete:"warning",needs_review:"warning",needs_selection:"warning",failed:"error"})[state]||"muted";
 return '<span class="operation-status status-'+tone+'">'+escapeHTML(label)+'</span>';
}
function operationFinished(job){return ["succeeded","incomplete","failed"].includes(job.state);}
function operationDismiss(job,label="Dismiss",corner=false){
 const finished=operationFinished(job);
 if(corner&&!finished)return "";
 const hint="Remove this result from Operations. Saved files stay in place.";
 const classes=corner?"tab-button operation-dismiss-close":finished?"tab-button operation-dismiss-result"+(job.state==="succeeded"?" completed":""):"tab-button";
 return '<button type="button" class="'+classes+'" data-import-dismiss="'+escapeAttr(job.id)+'"'+(finished?' title="'+hint+'"':"")+(corner?' aria-label="Dismiss result from Operations"':"")+'>'+(corner?'<span aria-hidden="true">×</span>':escapeHTML(finished?"Dismiss result":label))+'</button>';
}
function operationDismissHint(job){return operationFinished(job)?'<span class="secondary operation-dismiss-hint">Dismiss removes this result from Operations. Saved files stay in place.</span>':"";}
function operationBytes(value){
 const n=Math.max(0,Number(value)||0),units=["B","KiB","MiB","GiB","TiB"];
 const i=n>0?Math.min(4,Math.floor(Math.log(n)/Math.log(1024))):0;
 return formatNumber(Math.round(n/1024**i*10)/10)+" "+units[i];
}
function operationProgressMeter(current,total,unit="items",options={}){
 current=Math.max(0,Number(current)||0);total=Math.max(0,Number(total)||0);
 const known=options.known??total>0,format=options.bytes?operationBytes:formatNumber;
 const label=options.label||unit,percent=total>0?Math.min(100,Math.floor(current/total*100)):100;
 const text=known?format(current)+" / "+format(total)+" "+unit+" · "+percent+"%":format(current)+" "+unit;
 return '<span class="progress-count">'+escapeHTML(text)+'</span><progress aria-label="'+escapeAttr(label)+'" '+(known?'value="'+(total>0?Math.min(total,current):1)+'" max="'+(total||1)+'"':'')+'></progress>';
}
function importProgressUnit(stage){return stage==="bundling"?"releases":["writing","checking","matching","checking existing"].includes(stage)?"cards":"items";}
function libraryUploadStatus(){
 const p=libraryUI.uploadProgress||{};
 return "Uploading "+(p.total>0?operationBytes(p.loaded)+" / "+operationBytes(p.total)+" · "+Math.min(100,Math.floor(p.loaded/p.total*100))+"%":operationBytes(p.loaded)+" sent")+" — keep this page open until the upload finishes.";
}
function operationBusy(job){return job.state==="queued"||job.state==="planning"||job.state==="importing"||job.state==="enriching"||job.state==="exporting"}
async function libraryPollOperations(){
 if(libraryUI.polling)return;
 libraryUI.polling=true;
 try{
  const previous=JSON.stringify(libraryUI.jobs),previousSelection=libraryUI.selected,previousError=libraryUI.error;
  libraryUI.jobs=await fetchJSON("/api/operations");
  if(libraryUI.selected===null)libraryUI.selected=(libraryUI.jobs.find(operationBusy)||libraryUI.jobs[0])?.id||"";
  libraryUI.error="";
  const active=libraryUI.jobs.filter(operationBusy).length,waiting=libraryUI.jobs.filter(j=>["ready","needs_selection","needs_review","failed","incomplete"].includes(j.state)).length;
  $("operations-count").textContent=active?String(active):waiting?String(waiting):"";
  $("operations-status").textContent=active?active+" active / queued":waiting?waiting+" to review":"";
  for(const job of libraryUI.jobs){
   if(job.kind!=="export"&&(job.state==="succeeded"||(job.kind==="enrichment"&&["incomplete","failed"].includes(job.state)))&&!libraryUI.refreshed.has(job.id)){
    state.platforms=await fetchJSON("/api/platforms");state.platformsLoaded=true;
    $("platform-count").textContent=formatNumber(state.platforms.length);renderPlatforms();librarySyncNavigation();
    if(job.kind==="enrichment"&&(job.enrichment.platforms||[job.platform]).includes(state.platform)&&state.view==="browse"&&!state.exportMode&&!(typeof catalogEditorActive==="function"&&catalogEditorActive()))await selectPlatform(state.platform);
    libraryUI.refreshed.add(job.id);
   }
  }
  const selected=libraryUI.jobs.find(j=>j.id===libraryUI.selected);
  const stableExport=previous===JSON.stringify(libraryUI.jobs)&&previousSelection===libraryUI.selected&&!previousError;
  if(state.view==="operations"&&(!stableExport||(!selected&&enrichmentUI.draft)))libraryRenderOperations();
 }catch(error){libraryUI.error=error.message;if(state.view==="operations")libraryRenderOperations();}
 finally{
  libraryUI.polling=false;clearTimeout(libraryUI.timer);
  libraryUI.timer=setTimeout(libraryPollOperations,libraryUI.jobs.some(operationBusy)?1000:5000);
 }
}
function libraryOpenImport(){
 if(typeof catalogLeaveInline==="function"&&!catalogLeaveInline())return;
 if(typeof enrichmentUI!=="undefined"){enrichmentUI.version++;enrichmentUI.draft=null;}
 libraryUI.selected="";libraryUI.file=null;libraryNavigate("operations");libraryRenderOperations();
 $("dat-upload-file")?.focus();
}
function operationSummary(job){
 const x=job.summary;if(!x)return "";
 return '<div class="operation-summary"><div><strong>'+formatNumber(x.games)+'</strong><span>game cards</span></div><div><strong>'+formatNumber(x.releases)+'</strong><span>releases</span></div><div><strong>'+formatNumber(x.fingerprints)+'</strong><span>fingerprints</span></div></div>'+
 '<dl class="operation-facts"><dt>Platform</dt><dd>'+escapeHTML(x.platform)+'</dd><dt>Dataset</dt><dd>'+escapeHTML(x.dataset)+'</dd><dt>Source</dt><dd>'+escapeHTML(x.authority||x.format)+'</dd><dt>Changes</dt><dd>'+formatNumber(x.added)+' added · '+formatNumber(x.reconciled)+' reconciled</dd></dl>'+
 (x.conflicts?.length?'<div class="operation-error"><strong>Resolve these before importing</strong><ul>'+x.conflicts.map(v=>'<li>'+escapeHTML(v)+'</li>').join("")+'</ul></div>':"")+
 (x.samples?.length?'<details><summary>Sample releases</summary><ul>'+x.samples.map(v=>'<li>'+escapeHTML(v.Name||v.Description)+'</li>').join("")+'</ul></details>':"");
}
function libraryPlanDirty(job){
 const draft=libraryUI.drafts.get(job.id);
 return !!draft&&(draft.member!==(job.member||"")||draft.platform.trim()!==(job.platform||""));
}
function libraryUpdateApply(job){
 if(!job)return;const dirty=libraryPlanDirty(job);
 document.querySelectorAll("[data-import-apply]").forEach(button=>{button.disabled=dirty;});
 const hint=$("dat-apply-hint");if(hint)hint.textContent=dirty?"Options changed. Prepare the plan again before importing.":"The plan has not changed your cards.";
}
function operationDetail(job){
 if(job.kind==="export")return nativeExportDetail(job);
 if(job.kind==="enrichment")return enrichmentDetail(job);
 const busy=operationBusy(job),done=job.state==="succeeded",platform=job.summary?.platform||job.platform,canFetch=done&&catalogUI.enabled&&!!platform,p=job.progress||{},draft=libraryUI.drafts.get(job.id)||{},selected=draft.member??job.member??"";
 return '<article class="operation-detail">'+operationDismiss(job,"",true)+'<div class="operation-eyebrow">DAT IMPORT · '+operationStatus(job.state,operationLabels[job.state]||job.state)+'</div><h2>'+escapeHTML(job.filename)+'</h2>'+
 (job.member?'<p class="secondary">'+escapeHTML(job.member)+'</p>':"")+
 (busy?'<div class="operation-progress" role="status"><strong>'+escapeHTML(importStages[p.Stage]||p.Stage||"Preparing")+'</strong><span>'+escapeHTML(p.Item||"")+'</span>'+operationProgressMeter(p.Current,p.Total,importProgressUnit(p.Stage))+'<p>You can close your browser. This operation continues on the server.</p></div>':"")+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+
 (job.warning?'<p class="operation-error" role="status">'+escapeHTML(job.warning)+'</p>':"")+
 operationSummary(job)+
 (!busy&&!done?'<form id="dat-plan-form" data-operation="'+escapeAttr(job.id)+'"><details '+(!job.summary||job.state==="failed"?'open':'')+'><summary>'+(job.summary?'Change import options':'Import options')+'</summary>'+
 (job.members?.length?'<label>DAT in ZIP<select name="member" id="dat-member">'+(job.members.length>1&&!selected?'<option value="">Choose a DAT…</option>':"")+job.members.map(m=>'<option value="'+escapeAttr(m)+'" '+(m===selected?"selected":"")+'>'+escapeHTML(m)+'</option>').join("")+'</select></label>':"")+
 '<label>Platform name <span class="secondary">(leave empty to detect)</span><input id="dat-platform" name="platform" maxlength="150" value="'+escapeAttr(draft.platform??job.platform??"")+'" placeholder="'+escapeAttr(job.summary?.platform||"Detected from the DAT")+'"></label><button class="tab-button" type="submit">'+(job.state==="failed"?"Retry — prepare plan":"Prepare plan")+'</button></details></form>':"")+
 (canFetch?'<p><strong>Next step:</strong> add descriptions, artwork and other metadata. Choose your sources in the next screen.</p>':"")+
 '<div class="operation-actions">'+(canFetch?'<button class="catalog-primary" data-import-fetch="'+escapeAttr(job.id)+'">Fetch metadata</button>':"")+(job.state==="ready"?'<button class="catalog-primary" data-import-apply="'+job.id+'">Import into library</button><span class="secondary" id="dat-apply-hint">The plan has not changed your cards.</span>':"")+
 (done?'<button class="tab-button" data-import-open="'+job.id+'">Open '+escapeHTML(job.summary?.platform||"platform")+'</button><span class="secondary">Saved to your library.</span>':"")+
 (!busy?operationDismiss(job,"Discard upload"):"")+operationDismissHint(job)+'</div></article>';
}
function libraryJobMenu(open=false){
 if(!catalogUI.enabled)return state.platforms?.length?'<button class="tab-button" data-new-job="preview">Preview export</button>':"";
 const disabled=state.platforms?.length?"":" disabled";
 return '<details class="operations-add-job" id="operations-add-job" '+(open?"open":"")+'><summary class="tab-button" id="operations-add-job-toggle">Add job <span aria-hidden="true">▾</span></summary><div class="operation-job-menu" role="group" aria-label="New operation"><button type="button" id="operation-job-import" data-new-job="import">Import DAT</button><button type="button" id="operation-job-fetch" data-new-job="fetch"'+disabled+'>Fetch metadata</button><button type="button" id="operation-job-export" data-new-job="export"'+disabled+'>Export</button></div></details>';
}
function libraryStartJob(kind){
 const menu=$("operations-add-job");if(menu)menu.open=false;
 if(kind==="preview"&&state.platforms?.length){exportPreviewProfile();return;}
 if(!catalogUI.enabled)return;
 if(kind==="import")libraryOpenImport();
 else if(state.platforms?.length&&kind==="fetch")enrichmentOpen([]);
 else if(state.platforms?.length&&kind==="export")libraryNavigate("export");
}
function libraryRenderOperations(force=false){
 const pane=$("operations-pane");if(!pane)return;
 // Keep a partially completed options form stable while background polling runs.
 const focused=document.activeElement;
 if(!force&&focused&&pane.contains?.(focused)&&["INPUT","SELECT"].includes(focused.tagName))return;
 const selected=libraryUI.jobs.find(j=>j.id===libraryUI.selected),menu=$("operations-add-job"),menuOpen=!!menu?.open,menuFocus=menu?.contains?.(focused)?focused.id:"";
 pane.innerHTML='<div class="management-heading"><div><span class="operation-eyebrow">YOUR LIBRARY</span><h1>Operations</h1><p>Import, fetch metadata, export — return whenever you are ready.</p></div>'+libraryJobMenu(menuOpen)+'</div>'+
 (libraryUI.error?'<p class="operation-error" role="alert">'+escapeHTML(libraryUI.error)+'</p>':"")+
 '<div class="operations-layout"><aside class="operations-list" aria-label="Library operations">'+
 (libraryUI.jobs.length?libraryUI.jobs.map(j=>'<button class="operation-row '+(j.id===libraryUI.selected?"active":"")+'" data-operation-select="'+j.id+'"><strong>'+escapeHTML(operationTitle(j))+'</strong>'+operationStatus(j.state,operationLabel(j))+'<small>'+escapeHTML(j.platform||j.summary?.platform||j.member||"DAT import")+'</small></button>').join(""):'<p class="secondary">Your operations will appear here.</p>')+
 '<p class="secondary operations-lifetime">Progress stays with the running server, even when you close this browser. Inactive operations expire after 24 hours. Restarting the server clears this list.</p></aside>'+
 (selected?operationDetail(selected):typeof enrichmentUI!=="undefined"&&enrichmentUI.draft?enrichmentWizard():'<section class="operation-detail import-welcome"><h2>Import a DAT</h2><p>Start with a DAT. RomMeta prepares the game cards and fingerprints; your ROM files stay where they are.</p><form id="dat-upload-form"><label class="upload-label">Choose a DAT or ZIP<input type="file" id="dat-upload-file" name="file" accept=".dat,.xml,.zip" '+(libraryUI.uploading?"disabled":"")+'></label><p class="secondary">Recommended sources: <a href="https://datomatic.no-intro.org/" target="_blank" rel="noopener noreferrer">No-Intro (DAT-o-MATIC)</a> for cartridges, <a href="https://redump.info/downloads/" target="_blank" rel="noopener noreferrer">Redump</a> for discs, and <strong>MAME</strong> for arcade systems. Upload No-Intro, Redump or compatible XML DATs here; MAME listxml currently uses the CLI. ZIPs can contain several DATs. Up to 256 MiB.</p><button class="catalog-primary" type="submit" '+(libraryUI.uploading?"disabled":"")+'>'+(libraryUI.uploading?"Uploading…":"Upload & prepare")+'</button><p id="dat-upload-progress" role="status">'+(libraryUI.uploading?libraryUploadStatus():"Uploading prepares a plan. You choose when to import.")+'</p></form><details><summary>Prefer the command line?</summary><p>The CLI uses the same importer and catalog rules.</p><pre>rom-meta dat import &quot;&lt;file.dat-or-zip&gt;&quot; --data-root &quot;&lt;library&gt;&quot;\n# Add --apply after reviewing the plan.</pre><p class="secondary">Use the CLI for classified MAME listxml imports.</p></details></section>')+'</div>';
 pane.querySelectorAll("[data-new-job]").forEach(button=>button.addEventListener("click",()=>libraryStartJob(button.dataset.newJob)));
 if(menuFocus)$(menuFocus)?.focus({preventScroll:true});
 pane.querySelectorAll("[data-operation-select]").forEach(b=>b.addEventListener("click",()=>{libraryUI.selected=b.dataset.operationSelect;libraryRenderOperations();}));
 pane.querySelectorAll("[data-import-new]").forEach(b=>b.addEventListener("click",libraryOpenImport));
 $("dat-upload-form")?.addEventListener("submit",libraryUploadDAT);
 $("dat-upload-file")?.addEventListener("change",event=>{libraryUI.file=event.target.files?.[0]||null;});
 for(const id of ["dat-member","dat-platform"])$(id)?.addEventListener("input",()=>{libraryUI.drafts.set(selected.id,{member:$("dat-member")?.value||"",platform:$("dat-platform").value});libraryUpdateApply(selected);});
 libraryUpdateApply(selected);
 if(libraryUI.file&&$("dat-upload-progress")&&!libraryUI.uploading)$("dat-upload-progress").textContent="Selected: "+libraryUI.file.name;
 $("dat-upload-form")?.classList.toggle("hidden",!catalogUI.enabled);
 $("dat-plan-form")?.addEventListener("submit",async event=>{
  event.preventDefault();const job=libraryUI.jobs.find(j=>j.id===libraryUI.selected);if(!job)return;
  await libraryImportAction("/api/catalog/import-plan",{id:job.id,revision:job.revision,member:$("dat-member")?.value||"",platform:$("dat-platform").value});
 });
 pane.querySelectorAll("[data-import-apply]").forEach(b=>b.addEventListener("click",()=>libraryImportAction("/api/catalog/import-apply",{id:selected.id,revision:selected.revision})));
 pane.querySelectorAll("[data-import-dismiss]").forEach(b=>b.addEventListener("click",async()=>{await libraryImportAction("/api/operations/dismiss",{id:selected.id,revision:selected.revision});}));
 pane.querySelectorAll("[data-import-fetch]").forEach(b=>b.addEventListener("click",()=>libraryFetchImported(selected)));
 pane.querySelectorAll("[data-import-open]").forEach(b=>b.addEventListener("click",async()=>{
  await libraryOpenCompleted(selected);
 }));
 if(typeof enrichmentBind==="function")enrichmentBind(selected);
 if(typeof nativeExportBindOperation==="function")nativeExportBindOperation(selected);
 librarySyncNavigation();
}
function libraryFetchImported(job){
 const platform=job?.summary?.platform||job?.platform;
 if(job?.kind!=="dat-import"||job.state!=="succeeded"||!catalogUI.enabled||!platform)return;
 return enrichmentOpen([platform]);
}
async function libraryOpenCompleted(job){
 libraryNavigate("library");await selectPlatform(job.platform||job.summary?.platform);
 if(job.kind==="dat-import"&&job.state==="succeeded")await libraryImportAction("/api/operations/dismiss",{id:job.id,revision:job.revision});
}
document.addEventListener("click",event=>{const menu=$("operations-add-job");if(menu?.open&&!menu.contains(event.target))menu.open=false;});
document.addEventListener("keydown",event=>{const menu=$("operations-add-job");if(event.key==="Escape"&&menu?.open){menu.open=false;$("operations-add-job-toggle")?.focus();event.preventDefault();}});
function libraryRememberJob(job){
 const i=libraryUI.jobs.findIndex(j=>j.id===job.id);
 if(i<0)libraryUI.jobs.unshift(job);else libraryUI.jobs[i]=job;
 libraryUI.selected=job.id;
}
async function libraryImportAction(path,body){
 try{
  libraryUI.error="";
  const response=await catalogPost(path,body);
  if(response.dismissed){libraryUI.jobs=libraryUI.jobs.filter(j=>j.id!==body.id);libraryUI.selected="";}
  else {libraryUI.drafts.delete(body.id);libraryRememberJob(response);}
  libraryRenderOperations();libraryPollOperations();
 }catch(error){libraryUI.error=error.message;libraryRenderOperations();}
}
async function libraryUploadDAT(event){
 event.preventDefault();if(libraryUI.uploading)return;
 const file=$("dat-upload-file")?.files?.[0]||libraryUI.file;
 if(!file){libraryUI.error="Choose a DAT or ZIP file first.";libraryRenderOperations();return;}
 if(file.size>256*1024*1024){libraryUI.error="Choose a file up to 256 MiB.";libraryRenderOperations();return;}
 libraryUI.uploading=true;libraryUI.uploadProgress={loaded:0,total:0};libraryUI.error="";libraryRenderOperations();
 try{
  const job=await new Promise((resolve,reject)=>{
   const xhr=new XMLHttpRequest(),body=new FormData();body.append("file",file);
   xhr.open("POST","/api/catalog/import-upload");xhr.responseType="json";
   xhr.upload.onprogress=e=>{libraryUI.uploadProgress={loaded:e.loaded,total:e.lengthComputable?e.total:0};const el=$("dat-upload-progress");if(el)el.textContent=libraryUploadStatus();};
   xhr.onload=()=>xhr.status>=200&&xhr.status<300?resolve(xhr.response):reject(new Error(xhr.response?.error||"Upload failed. Try again."));
   xhr.onerror=()=>reject(new Error("Connection lost. Check Operations for a received upload before sending it again."));
   xhr.send(body);
  });
  libraryUI.file=null;libraryRememberJob(job);
 }catch(error){libraryUI.error=error.message;}
 finally{libraryUI.uploading=false;libraryRenderOperations();libraryPollOperations();}
}
const librarySettingsUI={sections:null,selected:"priorities",drafts:new Map(),loading:false,saving:false,error:"",notice:""};
function settingsSection(){return librarySettingsUI.sections?.find(s=>s.id===librarySettingsUI.selected)}
function settingsValue(section,field){const draft=librarySettingsUI.drafts.get(section.id);return draft&&Object.hasOwn(draft.values,field.id)?draft.values[field.id]:field.value}
function settingsChange(id,value){
 const section=settingsSection();if(!section||librarySettingsUI.saving)return;
 const field=section.fields.find(f=>f.id===id);if(!field)return;
 let draft=librarySettingsUI.drafts.get(section.id)||{revision:section.revision,values:{}};
 if(JSON.stringify(value)===JSON.stringify(field.value))delete draft.values[id];else draft.values[id]=value;
 if(Object.keys(draft.values).length)librarySettingsUI.drafts.set(section.id,draft);else librarySettingsUI.drafts.delete(section.id);
 librarySettingsUI.error="";librarySettingsUI.notice="";settingsUpdateActions();
}
function settingsUpdateActions(){
 const section=settingsSection(),dirty=!!section&&librarySettingsUI.drafts.has(section.id);
 if($("settings-save"))$("settings-save").disabled=!dirty||librarySettingsUI.saving;
 if($("settings-reset"))$("settings-reset").disabled=!dirty||librarySettingsUI.saving;
 if($("settings-feedback"))$("settings-feedback").textContent=librarySettingsUI.error||librarySettingsUI.notice||(dirty?"Unsaved changes":"");
}
function settingsListHTML(section,field){
 const selected=settingsValue(section,field)||[],choices=Array.from(new Set([...selected,...(field.value||[]),...(field.choices||[])]));
 return '<div class="settings-list">'+choices.map((source)=>{
  const index=selected.indexOf(source),enabled=index>=0,attributes=' data-setting-field="'+escapeAttr(field.id)+'" data-setting-source="'+escapeAttr(source)+'"';
  const label=field.id==="regions"?source:sourceLabel(source);
  return '<div class="source-order-row"><label><input type="checkbox" data-setting-check'+attributes+(enabled?' checked':'')+' '+(librarySettingsUI.saving?'disabled':'')+'> '+escapeHTML(label)+'</label>'+
   '<button class="tab-button" data-setting-move="up"'+attributes+' aria-label="Move '+escapeAttr(label)+' up in '+escapeAttr(field.label)+'" '+(index<=0||librarySettingsUI.saving?'disabled':'')+'>↑</button>'+
   '<button class="tab-button" data-setting-move="down"'+attributes+' aria-label="Move '+escapeAttr(label)+' down in '+escapeAttr(field.label)+'" '+(!enabled||index===selected.length-1||librarySettingsUI.saving?'disabled':'')+'>↓</button></div>';
 }).join("")+'<div class="settings-add"><input id="setting-add-'+escapeAttr(field.id)+'" aria-label="Add to '+escapeAttr(field.label)+'" placeholder="'+(field.id==="regions"?'Region name':'Source name, e.g. gamelist.my-list')+'"><button class="tab-button" data-setting-add="'+field.id+'">Add</button></div>'+
 (!selected.length?'<p class="secondary">No sources or regions selected.</p>':"")+'</div>';
}
function settingsListChange(fieldID,source,action){
 const section=settingsSection(),field=section?.fields.find(f=>f.id===fieldID);if(!field||librarySettingsUI.saving)return;
 let selected=(settingsValue(section,field)||[]).slice(),index=selected.indexOf(source);
 if(action==="enable"&&index<0)selected.push(source);
 else if(action==="disable")selected=selected.filter(v=>v!==source);
 else if(action==="up"&&index>0)[selected[index-1],selected[index]]=[selected[index],selected[index-1]];
 else if(action==="down"&&index>=0&&index<selected.length-1)[selected[index+1],selected[index]]=[selected[index],selected[index+1]];
 else return;
 settingsChange(fieldID,selected);libraryRenderSettings();
}
function libraryRenderSettings(){
 const pane=$("settings-pane");
 if(!librarySettingsUI.sections){pane.innerHTML='<h1>Settings</h1><p>'+escapeHTML(librarySettingsUI.error||"Loading library settings…")+'</p>';return}
 const section=settingsSection()||librarySettingsUI.sections[0];if(!section)return;
 librarySettingsUI.selected=section.id;
 pane.innerHTML='<div class="management-heading"><div><span class="operation-eyebrow">LIBRARY SETTINGS</span><h1>Settings</h1><p>Saved in this library and used by the CLI and browser.</p></div></div>'+
 '<div class="settings-layout"><nav class="settings-nav" aria-label="Settings sections">'+librarySettingsUI.sections.map(s=>'<button class="operation-row '+(s.id===section.id?'active':'')+'" data-settings-section="'+escapeAttr(s.id)+'">'+escapeHTML(s.title.startsWith("Connection · ")?sourceLabel(s.title.slice(13)):s.title)+(librarySettingsUI.drafts.has(s.id)?' · edited':'')+'</button>').join("")+
 '<p class="secondary">Export profile preferences are in Export. Service address and access protection are configured when starting serve.</p></nav>'+
 '<section class="operation-detail settings-editor"><h2>'+escapeHTML(section.title.startsWith("Connection · ")?sourceLabel(section.title.slice(13)):section.title)+'</h2><p>'+escapeHTML(section.help)+'</p>'+
 (section.status?'<p class="settings-credential-status">'+escapeHTML(section.status)+'</p>':"")+
 section.fields.map(field=>{
  const value=settingsValue(section,field),id='setting-'+field.id,label=' aria-label="'+escapeAttr(field.label)+'"';
  const control=field.kind==="list"?settingsListHTML(section,field):field.kind==="yaml"?'<textarea class="settings-yaml" id="'+id+'"'+label+' data-setting-input="'+field.id+'" spellcheck="false" rows="9" '+(librarySettingsUI.saving?'disabled':'')+'>'+escapeHTML(value||"")+'</textarea>':
   '<input id="'+id+'"'+label+' data-setting-input="'+field.id+'" type="'+(field.kind==="number"?'number':'text')+'" '+(field.kind==="number"?'min="0" max="1000" step="1"':'')+' value="'+escapeAttr(value??"")+'" '+(librarySettingsUI.saving?'disabled':'')+'>';
  return '<fieldset class="settings-field"><legend>'+escapeHTML(field.label)+'</legend>'+(field.help?'<p class="secondary settings-help">'+escapeHTML(field.help)+'</p>':"")+control+'</fieldset>';
 }).join("")+
 '<div class="operation-actions settings-savebar"><button class="catalog-primary" id="settings-save">'+(librarySettingsUI.saving?'Saving…':'Save changes')+'</button><button class="tab-button" id="settings-reset">Discard changes</button><button class="tab-button" id="settings-reload">Reload saved</button><span id="settings-feedback" role="status"></span></div>'+
 '<p class="secondary settings-file">Saved to '+escapeHTML(section.file)+'. Existing settings outside the edited fields are preserved.</p></section></div>';
 pane.querySelectorAll("[data-settings-section]").forEach(button=>button.addEventListener("click",()=>{librarySettingsUI.selected=button.dataset.settingsSection;librarySettingsUI.error="";librarySettingsUI.notice="";libraryRenderSettings();}));
 pane.querySelectorAll("[data-setting-input]").forEach(input=>input.addEventListener("input",()=>{
  const field=section.fields.find(f=>f.id===input.dataset.settingInput);
  settingsChange(field.id,field.kind==="number"?(input.value===""?0:Number(input.value)):input.value);
 }));
 pane.querySelectorAll("[data-setting-check]").forEach(input=>input.addEventListener("change",()=>settingsListChange(input.dataset.settingField,input.dataset.settingSource,input.checked?"enable":"disable")));
 pane.querySelectorAll("[data-setting-move]").forEach(button=>button.addEventListener("click",()=>settingsListChange(button.dataset.settingField,button.dataset.settingSource,button.dataset.settingMove)));
 pane.querySelectorAll("[data-setting-add]").forEach(button=>button.addEventListener("click",()=>{const source=$("setting-add-"+button.dataset.settingAdd).value.trim();if(source)settingsListChange(button.dataset.settingAdd,source,"enable");}));
 $("settings-save").addEventListener("click",librarySaveSettings);
 $("settings-reset").addEventListener("click",()=>{if(librarySettingsUI.saving)return;librarySettingsUI.drafts.delete(section.id);librarySettingsUI.error="";librarySettingsUI.notice="";libraryRenderSettings();});
 $("settings-reload").addEventListener("click",async()=>{
  if(librarySettingsUI.saving)return;
  if(librarySettingsUI.drafts.has(section.id)&&!window.confirm("Discard changes in this section and reload saved settings?"))return;
  librarySettingsUI.drafts.delete(section.id);await libraryLoadSettings(true);
 });
 settingsUpdateActions();
}
async function libraryLoadSettings(reload=false){
 if(!catalogUI.enabled){$("settings-pane").innerHTML='<h1>Settings</h1><p>This server is read-only. Start serve with --edit to manage library settings.</p>';return}
 if(librarySettingsUI.loading)return;
 if(librarySettingsUI.sections&&!reload){libraryRenderSettings();return}
 librarySettingsUI.loading=true;
 try{librarySettingsUI.sections=await fetchJSON("/api/catalog/settings");state.sourceDefaults=await fetchJSON("/api/export-options");librarySettingsUI.error="";}
 catch(error){librarySettingsUI.error=error.message;}
 finally{librarySettingsUI.loading=false;if(state.view==="settings")libraryRenderSettings();}
}
async function librarySaveSettings(){
 const section=settingsSection(),draft=section&&librarySettingsUI.drafts.get(section.id);
 if(!draft||librarySettingsUI.saving)return;
 librarySettingsUI.saving=true;librarySettingsUI.error="";librarySettingsUI.notice="";libraryRenderSettings();
 try{
  const result=await catalogPost("/api/catalog/settings",{section:section.id,revision:draft.revision,values:draft.values});
  // The save is already committed; a subsequent refresh failure must not claim it failed.
  for(const field of section.fields)if(Object.hasOwn(draft.values,field.id))field.value=draft.values[field.id];
  // Our own disjoint section save must not invalidate other drafts in the same file.
  // Older drafts still keep their revision so external changes remain conflicts.
  const savedRevision=draft.revision;
  for(const other of librarySettingsUI.sections){
   if(other.file!==section.file)continue;
   const pending=librarySettingsUI.drafts.get(other.id);
   if(pending?.revision===savedRevision)pending.revision=result.revision;
   if(other.revision===savedRevision)other.revision=result.revision;
  }
  section.revision=result.revision;librarySettingsUI.drafts.delete(section.id);
  librarySettingsUI.notice=result.warning?"Saved. "+result.warning:"Saved to library";
  try{
   librarySettingsUI.sections=await fetchJSON("/api/catalog/settings");
   state.sourceDefaults=await fetchJSON("/api/export-options");
   if(typeof resetPreviews==="function")resetPreviews();
  }catch(error){librarySettingsUI.notice+=". Could not refresh: "+error.message;}
 }catch(error){librarySettingsUI.error=error.message;}
 finally{librarySettingsUI.saving=false;if(state.view==="settings")libraryRenderSettings();}
}
window.addEventListener("beforeunload",event=>{if(librarySettingsUI.drafts.size||librarySettingsUI.saving){event.preventDefault();event.returnValue="";}});

$("operations-tab").addEventListener("click",()=>libraryNavigate("operations"));
$("settings-tab").addEventListener("click",()=>libraryNavigate("settings"));
$("library-import").addEventListener("click",libraryOpenImport);
document.querySelectorAll("[data-import-new]").forEach(b=>b.addEventListener("click",libraryOpenImport));

/* Platform actions stay beside the selection they affect. */
function librarySelectedPlatforms(){
 const available=new Set((state.platforms||[]).map(p=>p.platform));
 state.platformSelection=new Set([...(state.platformSelection||[])].filter(p=>available.has(p)));
 return [...state.platformSelection];
}
function libraryPlatformScope(){const selected=librarySelectedPlatforms();return selected.length?selected:state.platform?[state.platform]:[];}
function librarySyncPlatformSelection(){
 const selected=librarySelectedPlatforms(),enabled=!!catalogUI.enabled,all=state.platforms||[],scope=libraryPlatformScope();
 document.querySelectorAll("[data-platform-select]").forEach(input=>{
  input.checked=state.platformSelection.has(input.dataset.platformSelect);
  input.parentElement.classList.toggle("hidden",!enabled||state.exportMode);
 });
 $("platform-selection-toolbar")?.classList.toggle("hidden",!all.length||state.exportMode);
 $("platform-selection-options")?.classList.toggle("hidden",!enabled);
 const select=$("platform-select-all");if(select){select.checked=all.length>0&&selected.length===all.length;select.indeterminate=selected.length>0&&selected.length<all.length;}
 const count=$("platform-selected-count");if(count)count.textContent=selected.length?selected.length+" selected":"Current platform";
 for(const action of libraryPlatformActions(scope)){
  const button=$("platform-action-"+action.id);if(!button)continue;
  button.disabled=!action.enabled;button.title=action.detail;
 }
}
$("platform-list")?.addEventListener("change",event=>{
 const input=event.target.closest?.("[data-platform-select]");if(!input)return;
 librarySelectedPlatforms();if(input.checked)state.platformSelection.add(input.dataset.platformSelect);else state.platformSelection.delete(input.dataset.platformSelect);
 librarySyncPlatformSelection();
});
$("platform-select-all")?.addEventListener("change",event=>{
 state.platformSelection=new Set(event.target.checked?state.platforms.map(p=>p.platform):[]);librarySyncPlatformSelection();
});
function libraryPlatformActions(scope){
 const count=scope.length,description=count===1?scope[0]:count+" selected platforms",edit=!!catalogUI.enabled;
 return [
  {id:"fetch",detail:edit?"Fetch metadata · "+description:"Read-only server",enabled:edit&&count>0,run:()=>enrichmentOpen(scope)},
  {id:"fold",detail:edit?"Review duplicates · "+description:"Read-only server",enabled:edit&&count>0,run:()=>catalogBuildPlan(false,false,scope)},
  {id:"stats",detail:count===1?"Statistics · "+description:"Choose one platform for statistics",enabled:count===1,run:async()=>{if(!catalogLeaveInline())return;if(state.platform!==scope[0])await selectPlatform(scope[0]);setView("stats");}}
 ];
}
for(const id of ["fetch","fold","stats"])$("platform-action-"+id)?.addEventListener("click",async()=>{
 const action=libraryPlatformActions(libraryPlatformScope()).find(a=>a.id===id);if(action?.enabled)await action.run();
});

function libraryTheme(theme){
 document.documentElement.dataset.theme=theme;
 const b=$("theme-toggle");if(b){b.title="Switch to "+(theme==="light"?"dark":"light")+" theme";b.setAttribute("aria-label","Switch to "+(theme==="light"?"dark":"light")+" theme");}
 try{localStorage.setItem("rommeta-theme",theme);}catch{}
}
$("theme-toggle")?.addEventListener("click",()=>libraryTheme(document.documentElement.dataset.theme==="light"?"dark":"light"));
if($("theme-toggle"))libraryTheme(document.documentElement.dataset.theme||"dark");


librarySyncNavigation();
libraryPollOperations();
