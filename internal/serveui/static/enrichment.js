/* Platform enrichment uses server-owned operations; reconnect only reads state. */
const enrichmentUI={draft:null,version:0,busy:false,error:""};
const enrichmentTypes={metadata:"Metadata",artwork:"Artwork",video:"Video"};
const enrichmentStates={pending:"Waiting",running:"Running",ready:"Ready to enrich",enriching:"Enriching",succeeded:"Complete",incomplete:"Finished with issues",failed:"Could not finish",skipped:"Skipped"};
const enrichmentStages={starting:"Starting source",matching:"Matching games",scanning:"Reading catalog",reading:"Reading catalog",downloading:"Downloading",files:"Downloading files",indexing:"Updating library index",complete:"Complete"};
function enrichmentKey(job){return job.source+":"+job.type}
function enrichmentSelected(){return enrichmentUI.draft?.choices.filter(c=>c.available&&enrichmentUI.draft.selected.has(enrichmentKey(c)))||[]}
async function enrichmentOpen(platform=state.platform,previous){
 if(!platform||!catalogUI.enabled)return;
 if(typeof catalogLeaveInline==="function"&&!catalogLeaveInline())return;
 const version=++enrichmentUI.version;
 enrichmentUI.draft={platform,loading:true,choices:[],selected:new Set()};enrichmentUI.error="";
 libraryUI.selected="";libraryNavigate("operations");libraryRenderOperations();
 try{
  const options=await fetchJSON("/api/catalog/enrichment-options?"+new URLSearchParams({platform}));
  if(version!==enrichmentUI.version)return;
  const wanted=previous?new Set(previous.map(enrichmentKey)):null;
  enrichmentUI.draft={...options,loading:false,selected:new Set(options.choices.filter(c=>c.available&&(wanted?wanted.has(enrichmentKey(c)):c.default)).map(enrichmentKey))};
 }catch(error){if(version===enrichmentUI.version){enrichmentUI.draft.loading=false;enrichmentUI.error=error.message;}}
 if(version===enrichmentUI.version&&state.view==="operations")libraryRenderOperations();
}
function enrichmentWizard(){
 const f=enrichmentUI.draft,selected=enrichmentSelected();
 if(!f)return "";
 const names=[...new Set(f.choices.map(c=>c.source))];
 return '<section class="operation-detail enrichment-wizard"><div class="operation-eyebrow">PLATFORM ENRICHMENT</div><h2>'+escapeHTML(f.platform)+'</h2><p>Choose where to get metadata, artwork and videos. The selected tasks run in order, with metadata first.</p>'+
 (enrichmentUI.error?'<p class="operation-error" role="alert">'+escapeHTML(enrichmentUI.error)+'</p>':"")+
 (f.loading?'<p role="status">Reading source settings…</p>':'<form id="enrichment-plan-form"><fieldset '+(enrichmentUI.busy?'disabled':'')+'><legend>Sources and data</legend><div class="enrichment-choices">'+names.map(name=>{
  const choices=f.choices.filter(c=>c.source===name);
  return '<div class="enrichment-source"><strong>'+escapeHTML(sourceLabel(name))+'</strong><div>'+choices.map(c=>'<label title="'+escapeAttr(c.reason||"")+'"><input type="checkbox" data-enrichment-choice="'+escapeAttr(enrichmentKey(c))+'" '+(f.selected.has(enrichmentKey(c))?'checked ':'')+(!c.available?'disabled ':'')+'aria-label="'+escapeAttr(sourceLabel(name)+" "+enrichmentTypes[c.type])+'"> '+enrichmentTypes[c.type]+'</label>').join("")+'</div>'+
  (name==="hasheous"?'<small>Identity links from ROM hashes; does not provide descriptions.</small>':"")+
  (choices.some(c=>!c.available)?'<small>'+escapeHTML([...new Set(choices.filter(c=>!c.available).map(c=>c.reason))].map(reason=>{const types=choices.filter(c=>c.reason===reason);return (types.length===choices.length?"":types.map(c=>enrichmentTypes[c.type]).join(", ")+": ")+reason}).join(" "))+'</small>':"")+'</div>';
 }).join("")+'</div><p class="secondary">Defaults come from Settings → Enrichment. Credentials stay on the server. Saved source matches and existing media are kept; failed or unstarted work can be attempted again.</p><div class="operation-actions"><button type="submit" class="catalog-primary" id="enrichment-prepare" '+(!selected.length?'disabled':'')+'>'+(enrichmentUI.busy?'Preparing…':'Review tasks ('+selected.length+')')+'</button><button type="button" class="tab-button" id="enrichment-defaults">Use library defaults</button><button type="button" class="tab-button" id="enrichment-reload">Reload choices</button></div></fieldset></form>')+
 '<p class="secondary">Only this platform will change. Starting may download provider databases and media. After starting, you can close the browser and return to Operations.</p></section>';
}
async function enrichmentPrepare(event){
 event?.preventDefault();if(enrichmentUI.busy||!enrichmentSelected().length)return;
 const f=enrichmentUI.draft,jobs=enrichmentSelected().map(c=>({source:c.source,type:c.type}));
 enrichmentUI.busy=true;enrichmentUI.error="";libraryRenderOperations();
 try{
  const op=await catalogPost("/api/catalog/enrichment-plan",{platform:f.platform,revision:f.revision,jobs});
  enrichmentUI.draft=null;libraryRememberJob(op);
 }catch(error){enrichmentUI.error=error.message;}
 finally{enrichmentUI.busy=false;libraryRenderOperations();libraryPollOperations();}
}
function enrichmentProgress(job){
 const e=job.enrichment,p=e.progress||{},task=e.jobs[e.current],t=p.Transfers;
 const finished=e.jobs.filter(j=>["succeeded","incomplete","failed","skipped"].includes(j.state)).length;
 const bytes=n=>formatNumber(Math.round(Number(n||0)/104857.6)/10)+" MiB";
 return '<div class="operation-progress" role="status"><strong>'+escapeHTML(task?sourceLabel(task.source)+" · "+enrichmentTypes[task.type]:"Enriching platform")+'</strong><span>'+finished+' / '+e.jobs.length+' tasks finished · '+escapeHTML(enrichmentStages[p.Stage]||p.Stage||"Starting")+'</span>'+
 (p.Item?'<span class="secondary">'+escapeHTML(p.Item)+'</span>':"")+
 (p.Total>0?'<progress aria-label="Current stage" value="'+Math.min(p.Total,Math.max(0,p.Current||0))+'" max="'+p.Total+'"></progress><span>'+formatNumber(p.Current)+' / '+formatNumber(p.Total)+' in this stage</span>':p.BytesTotal>0?'<progress aria-label="Current download" value="'+Math.min(p.BytesTotal,Math.max(0,p.BytesCurrent||0))+'" max="'+p.BytesTotal+'"></progress>':'<progress aria-label="Enrichment in progress"></progress>')+
 (t?'<span>'+formatNumber(t.Downloaded)+' downloaded · '+formatNumber(t.Existing)+' existing · '+formatNumber(t.Failed)+' failed · '+formatNumber(Math.max(0,t.Queued-t.Downloaded-t.Failed))+' pending</span><span>'+bytes(t.Bytes)+' transferred</span>':p.BytesTotal>0?'<span>'+bytes(p.BytesCurrent)+' / '+bytes(p.BytesTotal)+'</span>':p.BytesCurrent>0?'<span>'+bytes(p.BytesCurrent)+' transferred</span>':"")+
 '<p>You can close your browser. Return to Operations to see this same task.</p></div>';
}
function enrichmentDetail(job){
 const busy=operationBusy(job),ready=job.state==="ready";
 return '<article class="operation-detail"><div class="operation-eyebrow">PLATFORM ENRICHMENT · '+escapeHTML(enrichmentStates[job.state]||job.state)+'</div><h2>'+escapeHTML(job.platform)+'</h2>'+
 (ready?'<p>These tasks use the library’s source settings. Saved source matches and existing media are kept. No provider requests or card changes have been made by this plan.</p>':busy?enrichmentProgress(job):'<p>'+(job.state==="succeeded"?'Selected tasks have finished.':'Some tasks could not finish; successful downloads and source matches remain saved.')+'</p>')+
 '<ol class="enrichment-tasks">'+job.enrichment.jobs.map(t=>{
  const c=t.counts||{},terminal=!["pending","running"].includes(t.state);
  return '<li><div><strong>'+escapeHTML(sourceLabel(t.source))+' · '+enrichmentTypes[t.type]+'</strong><span class="enrichment-task-state">'+escapeHTML(enrichmentStates[t.state]||t.state)+'</span></div>'+
  (terminal&&t.state!=="skipped"&&(t.state!=="failed"||Object.values(c).some(v=>v>0))?'<p class="secondary">'+(t.type==="metadata"?formatNumber(c.written||0)+' files written · '+formatNumber(c.matched||0)+' matched · '+formatNumber(c.not_found||0)+' not found · '+formatNumber(c.existing||0)+' existing'+(c.skipped?' · '+formatNumber(c.skipped)+' without lookup evidence':""):formatNumber(c.downloaded||0)+' downloads · '+formatNumber(c.existing||0)+' existing'+(c.written?' · '+formatNumber(c.written)+' artwork files written':""))+' · '+formatNumber(c.failed||0)+' failed</p>':"")+
  (t.error?'<p class="operation-error">'+escapeHTML(t.error)+'</p>':"")+'</li>';
 }).join("")+'</ol>'+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+
 (job.warning?'<p class="operation-error" role="status">'+escapeHTML(job.warning)+'</p>':"")+
 '<div class="operation-actions">'+(ready?'<button class="catalog-primary" data-enrichment-start="'+job.id+'">Start enrichment</button><button class="tab-button" data-enrichment-retry>Change selection</button>':!busy?'<button class="catalog-primary" data-import-open="'+job.id+'">Open platform</button><button class="tab-button" data-enrichment-retry>Prepare another pass</button>':"")+
 (!busy?'<button class="tab-button" data-import-dismiss="'+job.id+'">'+(ready?'Discard plan':'Dismiss result')+'</button>':"")+'</div></article>';
}
function enrichmentBind(selected){
 document.querySelectorAll("[data-enrichment-choice]").forEach(input=>input.addEventListener("change",()=>{
  if(enrichmentUI.busy||!enrichmentUI.draft)return;
  const key=input.dataset.enrichmentChoice;
  input.checked?enrichmentUI.draft.selected.add(key):enrichmentUI.draft.selected.delete(key);
  const count=enrichmentSelected().length;const button=$("enrichment-prepare");button.disabled=!count;button.textContent="Review tasks ("+count+")";
 }));
 $("enrichment-plan-form")?.addEventListener("submit",enrichmentPrepare);
 $("enrichment-defaults")?.addEventListener("click",()=>{const f=enrichmentUI.draft;f.selected=new Set(f.choices.filter(c=>c.available&&c.default).map(enrichmentKey));libraryRenderOperations();});
 $("enrichment-reload")?.addEventListener("click",()=>enrichmentOpen(enrichmentUI.draft.platform,enrichmentSelected()));
 document.querySelectorAll("[data-enrichment-start]").forEach(b=>b.addEventListener("click",async()=>{
  b.disabled=true;
  await libraryImportAction("/api/catalog/enrichment-start",{id:selected.id,revision:selected.revision});
 }));
 document.querySelectorAll("[data-enrichment-retry]").forEach(b=>b.addEventListener("click",()=>enrichmentOpen(selected.platform,selected.enrichment.jobs)));
}
$("platform-enrich")?.addEventListener("click",()=>enrichmentOpen());
