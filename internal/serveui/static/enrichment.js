/* Operations and the FIFO enrichment queue belong to the server. */
const enrichmentUI={draft:null,version:0,busy:false,error:"",tasks:new Map()};
const enrichmentTypes={metadata:"Metadata",artwork:"Artwork",video:"Video"};
const enrichmentStates={pending:"Waiting",queued:"Queued",running:"Running",ready:"Ready to fetch",needs_review:"Review settings",enriching:"Fetching metadata",succeeded:"Complete",incomplete:"Finished with issues",failed:"Could not finish",skipped:"Skipped"};
const enrichmentStages={starting:"Starting source",matching:"Matching games",scanning:"Reading catalog",reading:"Reading catalog",downloading:"Downloading",files:"Downloading files",indexing:"Updating library index",complete:"Complete"};
function enrichmentKey(job){return job.source+":"+job.type}
function enrichmentSelected(){return enrichmentUI.draft?.choices.filter(c=>c.available&&enrichmentUI.draft.selected.has(enrichmentKey(c)))||[]}
async function enrichmentOpen(platform=state.platform,previous,concurrency=0){
 if(!catalogUI.enabled)return;
 if(typeof catalogLeaveInline==="function"&&!catalogLeaveInline())return;
 const platforms=Array.isArray(platform)?platform:platform?[platform]:[];
 enrichmentUI.draft={platform:platforms[0]||"",platforms,concurrency,loading:false,choices:[],selected:new Set(previous?.map(enrichmentKey)||[])};
 enrichmentUI.error="";libraryUI.selected="";libraryNavigate("operations");
 await enrichmentLoadChoices(!previous);
}
async function enrichmentLoadChoices(defaults=false){
 const f=enrichmentUI.draft;if(!f)return;
 const version=++enrichmentUI.version;f.loading=true;enrichmentUI.error="";libraryRenderOperations(true);
 try{
  const options=await Promise.all(f.platforms.map(platform=>fetchJSON("/api/catalog/enrichment-options?"+new URLSearchParams({platform}))));
  if(version!==enrichmentUI.version)return;
  f.revision=options[0]?.revision||"";f.choices=[];
  const keys=[...new Set(options.flatMap(o=>o.choices.map(enrichmentKey)))];
  for(const key of keys){
   const choices=options.map(o=>o.choices.find(c=>enrichmentKey(c)===key)).filter(Boolean),base=choices[0],available=choices.filter(c=>c.available).length;
   f.choices.push({...base,available:available>0,platform_count:available,reason:[...new Set(choices.filter(c=>!c.available).map(c=>c.reason))].join("; ")});
  }
  if(defaults)f.selected=new Set(f.choices.filter(c=>c.available&&c.default).map(enrichmentKey));
 }catch(error){if(version===enrichmentUI.version)enrichmentUI.error=error.message;}
 finally{if(version===enrichmentUI.version){f.loading=false;if(state.view==="operations")libraryRenderOperations(true);}}
}
function enrichmentWizard(){
 const f=enrichmentUI.draft,selected=enrichmentSelected();if(!f)return "";
 const names=[...new Set(f.choices.map(c=>c.source))],platforms=state.platforms||[];
 return '<section class="operation-detail enrichment-wizard"><h2>Fetch metadata</h2><p>Choose platforms and sources. Tasks run in order; downloads within each task can run in parallel.</p>'+
 (enrichmentUI.error?'<p class="operation-error" role="alert">'+escapeHTML(enrichmentUI.error)+'</p>':"")+
 '<form id="enrichment-plan-form"><fieldset '+(enrichmentUI.busy?'disabled':'')+'><legend>Platforms ('+f.platforms.length+')</legend><div class="operation-actions"><button type="button" class="tab-button" data-enrichment-platforms="all">Select all</button><button type="button" class="tab-button" data-enrichment-platforms="none">Clear</button></div><div class="enrichment-platforms">'+platforms.map(p=>'<label><input type="checkbox" data-enrichment-platform="'+escapeAttr(p.platform)+'" '+(f.platforms.includes(p.platform)?'checked':'')+'> '+escapeHTML(p.platform)+'</label>').join("")+'</div></fieldset>'+
 '<fieldset '+(enrichmentUI.busy||f.loading?'disabled':'')+'><legend>Sources and data</legend>'+
 (f.loading?'<p role="status">Reading source settings…</p>':!f.platforms.length?'<p>Select at least one platform.</p>':'<div class="enrichment-choices">'+names.map(name=>{
 const choices=f.choices.filter(c=>c.source===name);
 return '<div class="enrichment-source"><strong>'+escapeHTML(sourceLabel(name))+'</strong><div>'+choices.map(c=>'<label title="'+escapeAttr(c.reason||"")+'"><input type="checkbox" data-enrichment-choice="'+escapeAttr(enrichmentKey(c))+'" '+(f.selected.has(enrichmentKey(c))?'checked ':'')+(!c.available?'disabled ':'')+'> '+enrichmentTypes[c.type]+(f.platforms.length>1?' <small>('+c.platform_count+'/'+f.platforms.length+')</small>':'')+'</label>').join("")+'</div>'+
 (choices.some(c=>c.reason)?'<small>'+escapeHTML([...new Set(choices.map(c=>c.reason).filter(Boolean))].join(" "))+'</small>':"")+'</div>';
 }).join("")+'</div>')+
 '<label class="enrichment-workers">Parallel requests / downloads<select id="enrichment-concurrency">'+[0,1,2,4,8,16,32].map(n=>'<option value="'+n+'" '+(f.concurrency===n?'selected':'')+'>'+(n?n+' worker'+(n===1?'':'s'):'Library / provider defaults')+'</option>').join("")+'</select><small>Applied within each source task. Provider limits still apply.</small></label>'+
 '<p class="secondary">Saved matches and existing media are kept. Unavailable sources are skipped for the affected platforms.</p><div class="operation-actions"><button type="submit" class="catalog-primary" id="enrichment-prepare" '+(!selected.length||f.loading?'disabled':'')+'>'+(enrichmentUI.busy?'Preparing…':'Review tasks')+'</button><button type="button" class="tab-button" id="enrichment-defaults">Use library defaults</button></div></fieldset></form><p class="secondary">Once queued, work continues on this server after the browser is closed.</p></section>';
}
async function enrichmentPrepare(event){
 event?.preventDefault();if(enrichmentUI.busy||enrichmentUI.draft.loading||!enrichmentSelected().length)return;
 const f=enrichmentUI.draft,jobs=enrichmentSelected().map(c=>({source:c.source,type:c.type}));
 enrichmentUI.busy=true;enrichmentUI.error="";libraryRenderOperations(true);
 try{const op=await catalogPost("/api/catalog/enrichment-plan",{platform:f.platforms[0],platforms:f.platforms,revision:f.revision,concurrency:f.concurrency,jobs});enrichmentUI.draft=null;libraryRememberJob(op);}
 catch(error){enrichmentUI.error=error.message;}
 finally{enrichmentUI.busy=false;libraryRenderOperations(true);libraryPollOperations();}
}
function enrichmentProgress(job){
 const e=job.enrichment,p=e.progress||{},task=e.jobs[e.current],t=p.Transfers;
 const bytes=n=>formatNumber(Math.round(Number(n||0)/104857.6)/10)+" MiB";
 const total=t?t.Queued:p.Total,current=t?t.Downloaded+t.Failed:p.Current;
 return '<div class="operation-progress" role="status"><strong>'+escapeHTML(enrichmentStages[p.Stage]||p.Stage||"Starting")+'</strong>'+
 (total>0?'<progress aria-label="Current stage" value="'+Math.min(total,Math.max(0,current||0))+'" max="'+total+'"></progress>':'<progress aria-label="Fetch in progress"></progress>')+
 (t?'<span>'+formatNumber(t.Downloaded)+' downloaded · '+formatNumber(t.Existing)+' existing · '+formatNumber(t.Failed)+' failed · '+formatNumber(Math.max(0,t.Queued-t.Downloaded-t.Failed))+' pending</span><small>'+bytes(t.Bytes)+' transferred · '+formatNumber(t.Queued)+' files discovered; total may grow while scanning</small>':p.Total>0?'<span>'+formatNumber(p.Current)+' / '+formatNumber(p.Total)+' games</span>':p.BytesTotal>0?'<span>'+bytes(p.BytesCurrent)+' / '+bytes(p.BytesTotal)+'</span>':"")+
 (p.Item?'<span class="secondary">'+escapeHTML(p.Item)+'</span>':"")+
 '<div class="enrichment-transfers">'+Object.entries(e.workers||{}).map(([id,w])=>'<div><span>Worker '+Number(id)+'</span><span>'+escapeHTML(w.Item||"Working")+'</span>'+(w.BytesTotal>0?'<progress aria-label="Worker '+Number(id)+' download" value="'+Math.max(0,w.BytesCurrent||0)+'" max="'+w.BytesTotal+'"></progress><small>'+bytes(w.BytesCurrent)+' / '+bytes(w.BytesTotal)+'</small>':'')+'</div>').join("")+'</div><small>Up to '+(task?.workers||1)+' worker'+((task?.workers||1)===1?'':'s')+' for this task.</small></div>';
}
function enrichmentTaskResult(t){
 const c=t.counts||{};
 return '<p>'+escapeHTML(enrichmentStates[t.state]||t.state)+'</p>'+
 (["pending","running","skipped"].includes(t.state)?"":'<dl class="operation-facts">'+(t.type==="metadata"?'<dt>Matched</dt><dd>'+formatNumber(c.matched||0)+'</dd><dt>Not found</dt><dd>'+formatNumber(c.not_found||0)+'</dd><dt>Files written</dt><dd>'+formatNumber(c.written||0)+'</dd>':'<dt>Downloaded</dt><dd>'+formatNumber(c.downloaded||0)+'</dd>')+'<dt>Existing</dt><dd>'+formatNumber(c.existing||0)+'</dd><dt>Failed</dt><dd>'+formatNumber(c.failed||0)+'</dd></dl>')+
 (t.error?'<p class="operation-error">'+escapeHTML(t.error)+'</p>':"");
}
function enrichmentDetail(job){
 const e=job.enrichment,busy=operationBusy(job),ready=job.state==="ready",queued=job.state==="queued",platforms=e.platforms||[job.platform];
 const index=enrichmentUI.tasks.has(job.id)?enrichmentUI.tasks.get(job.id):Math.max(0,e.current||0),task=e.jobs[index]||e.jobs[0];
 const finished=e.jobs.filter(t=>!["pending","running"].includes(t.state)).length;
 const queue=libraryUI.jobs.filter(j=>j.state==="queued").sort((a,b)=>(a.queued_at||"").localeCompare(b.queued_at||""));
 return '<article class="operation-detail enrichment-detail"><div class="operation-eyebrow">FETCH METADATA · '+escapeHTML(enrichmentStates[job.state]||job.state)+'</div><h2>'+escapeHTML(platforms.length===1?platforms[0]:platforms.length+" platforms")+'</h2>'+
 (platforms.length>1?'<details><summary>Selected platforms</summary><ul>'+platforms.map(p=>'<li>'+escapeHTML(p)+'</li>').join("")+'</ul></details>':"")+
 (queued?'<p class="queue-status" role="status">Queue position '+Math.max(1,queue.findIndex(j=>j.id===job.id)+1)+'. Starts automatically when the current library operation finishes. You can close the browser.</p>':ready?'<p>Review the tasks, then add them to the server queue. No downloads have started.</p>':'<p>'+finished+' / '+e.jobs.length+' tasks finished. Progress remains available in Operations.</p>')+
 '<div class="enrichment-layout"><nav aria-label="Fetch stages"><ol class="enrichment-tasks">'+e.jobs.map((t,i)=>'<li><button class="enrichment-stage '+(i===index?'active':'')+'" data-enrichment-task="'+i+'"><span>'+escapeHTML(sourceLabel(t.source))+' · '+enrichmentTypes[t.type]+'</span>'+(platforms.length>1?'<small>'+escapeHTML(t.platform)+'</small>':"")+'<small>'+escapeHTML(enrichmentStates[t.state]||t.state)+'</small></button></li>').join("")+'</ol></nav><section class="enrichment-stage-detail"><h3>'+escapeHTML(task?sourceLabel(task.source)+" · "+enrichmentTypes[task.type]:"Tasks")+'</h3>'+
 (task?'<p class="secondary">'+escapeHTML(task.platform||job.platform)+' · '+(task.workers||1)+' worker'+((task.workers||1)===1?'':'s')+'</p>'+((job.state==="enriching"&&index===e.current)?enrichmentProgress(job):enrichmentTaskResult(task)):"")+
 (job.state==="enriching"&&index!==e.current?'<button class="tab-button" data-enrichment-follow>Show current task</button>':"")+'</section></div>'+
 (job.error?'<p class="operation-error" role="alert">'+escapeHTML(job.error)+'</p>':"")+(job.warning?'<p class="operation-error">'+escapeHTML(job.warning)+'</p>':"")+
 '<div class="operation-actions">'+(ready?'<button class="catalog-primary" data-enrichment-start="'+job.id+'">Add to queue</button><button class="tab-button" data-enrichment-retry>Change selection</button>':!busy?'<button class="tab-button" data-import-open="'+job.id+'">Open platform</button><button class="tab-button" data-enrichment-retry>'+(job.state==='needs_review'?'Change selection':'Prepare another pass')+'</button>':"")+
 (!busy||queued?'<button class="tab-button" data-import-dismiss="'+job.id+'">'+(queued?'Remove from queue':ready?'Discard plan':'Dismiss result')+'</button>':"")+'</div></article>';
}
function enrichmentBind(selected){
 document.querySelectorAll("[data-enrichment-platform]").forEach(input=>input.addEventListener("change",()=>{const f=enrichmentUI.draft,p=input.dataset.enrichmentPlatform;if(input.checked)f.platforms.push(p);else f.platforms=f.platforms.filter(v=>v!==p);enrichmentLoadChoices(!f.choices.length);}));
 document.querySelectorAll("[data-enrichment-platforms]").forEach(b=>b.addEventListener("click",()=>{enrichmentUI.draft.platforms=b.dataset.enrichmentPlatforms==="all"?state.platforms.map(p=>p.platform):[];enrichmentLoadChoices(!enrichmentUI.draft.choices.length);}));
 document.querySelectorAll("[data-enrichment-choice]").forEach(input=>input.addEventListener("change",()=>{const f=enrichmentUI.draft,key=input.dataset.enrichmentChoice;input.checked?f.selected.add(key):f.selected.delete(key);$("enrichment-prepare").disabled=!enrichmentSelected().length;}));
 $("enrichment-concurrency")?.addEventListener("change",e=>{enrichmentUI.draft.concurrency=Number(e.target.value);});
 $("enrichment-plan-form")?.addEventListener("submit",enrichmentPrepare);
 $("enrichment-defaults")?.addEventListener("click",()=>{const f=enrichmentUI.draft;f.selected=new Set(f.choices.filter(c=>c.available&&c.default).map(enrichmentKey));f.concurrency=0;libraryRenderOperations(true);});
 document.querySelectorAll("[data-enrichment-start]").forEach(b=>b.addEventListener("click",async()=>{b.disabled=true;b.textContent="Adding to queue…";await libraryImportAction("/api/catalog/enrichment-start",{id:selected.id,revision:selected.revision});}));
 document.querySelectorAll("[data-enrichment-retry]").forEach(b=>b.addEventListener("click",()=>enrichmentOpen(selected.enrichment.platforms||[selected.platform],selected.enrichment.jobs,selected.enrichment.concurrency||0)));
 document.querySelectorAll("[data-enrichment-task]").forEach(b=>b.addEventListener("click",()=>{enrichmentUI.tasks.set(selected.id,Number(b.dataset.enrichmentTask));libraryRenderOperations(true);}));
 document.querySelectorAll("[data-enrichment-follow]").forEach(b=>b.addEventListener("click",()=>{enrichmentUI.tasks.delete(selected.id);libraryRenderOperations(true);}));
 document.querySelectorAll("[data-enrichment-new]").forEach(b=>b.addEventListener("click",()=>enrichmentOpen([])));
}
$("platform-enrich")?.addEventListener("click",()=>{platformMenuClose();enrichmentOpen();});
$("platform-fetch")?.addEventListener("click",()=>enrichmentOpen([]));
