
function exportIssueDraft(job){
 let d=nativeExportUI.reviewDrafts.get(job.id);
 const saved=JSON.stringify([job.export.excluded||[],job.export.bindings||[]]);
 if(!d||d.revision!==job.revision||d.saved!==saved){d={saved,revision:job.revision,excluded:new Set(job.export.excluded||[]),bindings:new Map((job.export.bindings||[]).map(x=>[x.id,{...x}])),suggestionPending:new Map(),suggestionErrors:new Map(),suggestionBatch:null,query:"",page:0};nativeExportUI.reviewDrafts.set(job.id,d);}
 return d;
}
function exportSuggestionsPending(job){const d=exportIssueDraft(job);return !!d.suggestionBatch||d.suggestionPending.size>0;}
function exportSingleSuggestions(job,d=exportIssueDraft(job)){
 return (job.export.totals?.issues||[]).filter(x=>x.can_bind&&x.suggestions?.length===1&&!d.bindings.has(x.id)&&!d.excluded.has(x.id)&&!d.suggestionPending.has(x.id));
}
function exportSuggestionCurrent(job,d){
 const current=libraryUI.jobs.find(x=>x.id===job.id);
 return !!current&&current.revision===job.revision&&["ready","needs_review"].includes(current.state)&&exportIssueDraft(current)===d;
}
function exportCancelSuggestions(d){d.suggestionBatch=null;d.suggestionPending.clear();d.suggestionErrors.clear();}
function exportBindingKey(x){return JSON.stringify([x.game,x.release||"",x.game_revision]);}
function exportReviewDirty(job){
 const d=exportIssueDraft(job),saved=job.export.excluded||[],bindings=job.export.bindings||[];
 return d.excluded.size!==saved.length||saved.some(id=>!d.excluded.has(id))||d.bindings.size!==bindings.length||bindings.some(x=>!d.bindings.has(x.id)||exportBindingKey(x)!==exportBindingKey(d.bindings.get(x.id)));
}
function exportReviewHint(job){const d=exportIssueDraft(job);return exportReviewDirty(job)?"Choices changed. Review the updated plan before applying.":d.bindings.size+" manually assigned · "+d.excluded.size+" excluded from this export.";}
function exportIssueRow(x,d,review){
 const binding=d.bindings.get(x.id),game=binding?.game||x.game,release=binding?binding.release:x.release,excluded=d.excluded.has(x.id),disabled=excluded?'disabled ':'';
 const choose=review&&x.can_bind?'<button type="button" class="tab-button" data-export-match="'+escapeAttr(x.id)+'" '+disabled+'>'+(binding?'Change game':'Choose game')+'</button>':"";
 const reset=review&&binding?'<button type="button" class="tab-button" data-export-match-reset="'+escapeAttr(x.id)+'" '+disabled+'>Reset match</button>':"";
 const suggestions=!binding&&x.suggestions?.length;
 return '<div class="export-issue'+(excluded?' is-excluded':'')+'"><input type="checkbox" class="export-issue-include" data-export-include="'+escapeAttr(x.id)+'" '+(!excluded?'checked ':'')+(!review?'disabled ':'')+'aria-label="'+escapeAttr('Include '+x.rom+' in export')+'" title="Include in export"><div class="export-issue-content"><div class="export-issue-heading"><strong>'+escapeHTML(x.rom)+'</strong><span class="secondary">'+escapeHTML(excluded?"Excluded":binding?"Manual choice":x.matched_by==="manual"?"Automatic match will be restored":suggestions?"Needs confirmation":x.status)+'</span></div>'+
 '<small>'+escapeHTML(x.platform)+' · '+escapeHTML(binding?"Selected by you; not verified by fingerprint":x.reason||x.warnings?.join("; ")||"Review match evidence")+'</small>'+
 (game?'<small>'+(binding?'Selected: ':'Matched: ')+escapeHTML(game)+(release?' / '+escapeHTML(release):binding?' · game only':"")+'</small>':"")+
 (!binding&&!suggestions&&x.candidates?.length?'<small>Candidates: '+escapeHTML(x.candidates.join("; "))+'</small>':"")+
 (suggestions?'<div class="export-suggestions"><small>Suggested by name</small><div class="export-suggestion-choices">'+x.suggestions.map(v=>'<div class="export-suggestion-option"><strong>'+escapeHTML(v.title)+'</strong> '+(review&&x.can_bind?'<button type="button" class="catalog-primary export-accept-suggestion" aria-label="'+escapeAttr('Accept match: '+v.title)+'" data-export-suggestion="'+escapeAttr(x.id)+'" data-export-suggestion-game="'+escapeAttr(v.game)+'" '+(excluded||d.suggestionPending?.has(x.id)?'disabled':'')+'>'+(d.suggestionPending?.has(x.id)?'Accepting…':'<span aria-hidden="true">✓</span> Accept match')+'</button>':'')+'</div>').join("")+choose+'</div></div>':"")+
 (d.suggestionErrors?.get(x.id)?'<small class="operation-error" role="alert">'+escapeHTML(d.suggestionErrors.get(x.id))+'</small>':"")+
 (!suggestions&&(choose||reset)?'<div class="export-issue-actions">'+choose+reset+'</div>':"")+'</div></div>';
}
function exportIssues(job){
 const issues=job.export.totals?.issues||[],d=exportIssueDraft(job),review=["ready","needs_review"].includes(job.state),q=d.query.toLowerCase();
 const filtered=issues.filter(x=>[x.rom,d.bindings.get(x.id)?.game,x.game,x.reason,x.platform,...(x.candidates||[])].join(" ").toLowerCase().includes(q)),pages=Math.max(1,Math.ceil(filtered.length/40));d.page=Math.min(d.page,pages-1);
 const singles=exportSingleSuggestions(job,d),batch=d.suggestionBatch;
 const bulk=review&&(singles.length||batch)?'<button type="button" class="catalog-primary" data-export-accept-single '+(exportSuggestionsPending(job)?'disabled ':'')+'title="All pages; existing choices and exclusions are kept" aria-live="polite">'+(batch?'Accepting '+batch.done+' / '+batch.total+'…':'Accept all with one candidate ('+singles.length+')')+'</button>':"";
 return '<section class="export-review"><h3>Inputs needing attention ('+issues.length+')</h3>'+
 (!issues.length?'<p>No unresolved or ambiguous ROMs. Recognized inputs are included automatically.</p>':'<p>Confirm matches for the checked files.</p><div class="export-issue-tools"><input id="export-issue-search" aria-label="Filter problem inputs" placeholder="Filter filenames or reasons" value="'+escapeAttr(d.query)+'">'+bulk+(review?'<button type="button" class="tab-button" data-export-uncheck-unresolved>Uncheck unresolved</button><button type="button" class="tab-button" data-export-restore-all title="Clear manual matches and check all files">Reset choices</button>':'')+'</div><div class="export-issues">'+filtered.slice(d.page*40,(d.page+1)*40).map(x=>exportIssueRow(x,d,review)).join("")+'</div>'+(pages>1?'<div class="operation-actions"><button class="tab-button" data-export-issue-page="-1" '+(!d.page?'disabled':'')+'>Previous</button><span>'+(d.page+1)+' / '+pages+' · '+filtered.length+' inputs</span><button class="tab-button" data-export-issue-page="1" '+(d.page===pages-1?'disabled':'')+'>Next</button></div>':""))+'</section>';
}
function exportBindReview(job){
 const d=exportIssueDraft(job);
 const update=()=>{document.querySelectorAll("[data-native-export-apply]").forEach(b=>b.disabled=exportReviewDirty(job)||exportSuggestionsPending(job));document.querySelectorAll("[data-export-review-update]").forEach(b=>b.disabled=!exportReviewDirty(job)||exportSuggestionsPending(job));const hint=$("export-review-hint");if(hint)hint.textContent=exportReviewHint(job);};
 document.querySelectorAll("[data-export-include]").forEach(c=>c.addEventListener("change",()=>{d.suggestionPending.delete(c.dataset.exportInclude);d.suggestionErrors.delete(c.dataset.exportInclude);if(c.checked)d.excluded.delete(c.dataset.exportInclude);else{d.excluded.add(c.dataset.exportInclude);d.bindings.delete(c.dataset.exportInclude);}libraryRenderOperations(true);Array.from(document.querySelectorAll("[data-export-include]")).find(next=>next.dataset.exportInclude===c.dataset.exportInclude)?.focus({preventScroll:true});}));
 document.querySelectorAll("[data-export-accept-single]").forEach(b=>b.addEventListener("click",()=>exportAcceptSingleSuggestions(job)));
 document.querySelectorAll("[data-export-suggestion]").forEach(b=>b.addEventListener("click",()=>exportAcceptSuggestion(job,b.dataset.exportSuggestion,b.dataset.exportSuggestionGame)));
 document.querySelectorAll("[data-export-match]").forEach(b=>b.addEventListener("click",()=>exportOpenMatch(job,b.dataset.exportMatch)));
 document.querySelectorAll("[data-export-match-reset]").forEach(b=>b.addEventListener("click",()=>{d.suggestionPending.delete(b.dataset.exportMatchReset);d.bindings.delete(b.dataset.exportMatchReset);libraryRenderOperations();}));
 document.querySelectorAll("[data-export-uncheck-unresolved]").forEach(b=>b.addEventListener("click",()=>{exportCancelSuggestions(d);for(const x of job.export.totals.issues||[])if(x.status!=="matched"&&!d.bindings.has(x.id))d.excluded.add(x.id);libraryRenderOperations();}));
 document.querySelectorAll("[data-export-restore-all]").forEach(b=>b.addEventListener("click",()=>{exportCancelSuggestions(d);d.excluded.clear();d.bindings.clear();libraryRenderOperations();}));
 document.querySelectorAll("[data-export-issue-page]").forEach(b=>b.addEventListener("click",()=>{d.page+=Number(b.dataset.exportIssuePage);libraryRenderOperations();}));
 $("export-issue-search")?.addEventListener("input",event=>{d.query=event.target.value;d.page=0;const pos=event.target.selectionStart;event.target.blur();libraryRenderOperations();$("export-issue-search")?.focus();$("export-issue-search")?.setSelectionRange(pos,pos);});
 document.querySelectorAll("[data-export-review-update]").forEach(b=>b.addEventListener("click",async()=>{if(exportSuggestionsPending(job)||!exportReviewDirty(job))return;b.disabled=true;await libraryImportAction("/api/export/review",{id:job.id,revision:job.revision,excluded:[...d.excluded],bindings:[...d.bindings.values()]});}));
 update();
}
async function exportAcceptSuggestion(job,id,game){
 const issue=job.export.totals.issues.find(x=>x.id===id),d=exportIssueDraft(job);
 if(d.excluded.has(id)||!exportSuggestionCurrent(job,d)||!issue?.can_bind||!issue.suggestions?.some(x=>x.game===game)||d.suggestionPending.has(id))return;
 const request={};d.suggestionPending.set(id,request);d.suggestionErrors.delete(id);libraryRenderOperations();
 await exportResolveSuggestion(job,d,{id,game,request});
}
async function exportResolveSuggestion(job,d,{id,game,request}){
 try{
  const data=await fetchJSON("/api/export/match-options?"+new URLSearchParams({id:job.id,revision:job.revision,input:id,game}));
  if(d.suggestionPending.get(id)!==request||d.excluded.has(id)||!exportSuggestionCurrent(job,d))return;
  if(data.target?.game!==game||!data.target.game_revision)throw new Error("Suggested card is no longer available.");
  d.bindings.set(id,{id,game,release:"",game_revision:data.target.game_revision});d.excluded.delete(id);
 }catch(error){if(d.suggestionPending.get(id)===request&&exportSuggestionCurrent(job,d))d.suggestionErrors.set(id,error.message);}
 finally{if(d.suggestionPending.get(id)===request)d.suggestionPending.delete(id);libraryRenderOperations();}
}
async function exportAcceptSingleSuggestions(job){
 const d=exportIssueDraft(job);
 if(!exportSuggestionCurrent(job,d)||exportSuggestionsPending(job))return;
 const choices=exportSingleSuggestions(job,d).map(x=>({id:x.id,game:x.suggestions[0].game,request:{}}));
 if(!choices.length)return;
 const batch={done:0,total:choices.length};d.suggestionBatch=batch;
 for(const x of choices){d.suggestionPending.set(x.id,x.request);d.suggestionErrors.delete(x.id);}
 libraryRenderOperations();let next=0;
 const worker=async()=>{
  while(next<choices.length&&d.suggestionBatch===batch&&exportSuggestionCurrent(job,d)){
   const x=choices[next++];
   if(d.suggestionPending.get(x.id)===x.request)await exportResolveSuggestion(job,d,x);
   batch.done++;libraryRenderOperations();
  }
 };
 try{await Promise.all(Array.from({length:Math.min(4,choices.length)},worker));}
 finally{
  for(const x of choices)if(d.suggestionPending.get(x.id)===x.request)d.suggestionPending.delete(x.id);
  if(d.suggestionBatch===batch)d.suggestionBatch=null;
  libraryRenderOperations();
 }
}
function exportOpenMatch(job,id){
 exportIssueDraft(job).suggestionPending.delete(id);
 const issue=job.export.totals.issues.find(x=>x.id===id);
 if(!issue?.can_bind||exportIssueDraft(job).excluded.has(id))return;
 nativeExportUI.match={job,id,rom:issue.rom,query:"",offset:0,games:[],total:0,target:null,release:"",returnFocus:document.activeElement};
 exportLoadMatch();
}
function exportCloseMatch(){
 const m=nativeExportUI.match,previous=m?.returnFocus;
 clearTimeout(m?.timer);m?.controller?.abort();
 nativeExportUI.match=null;$("export-match-dialog").classList.add("hidden");$("app").inert=false;
 if(previous?.isConnected)previous.focus();
}
function exportSearchMatch(immediate=false){
 const m=nativeExportUI.match;if(!m)return;
 clearTimeout(m.timer);m.controller?.abort();m.request=(m.request||0)+1;
 m.query=$("export-match-query").value;m.offset=0;m.target=null;m.loading=true;m.error="";
 exportRenderMatch();
 if(immediate)return exportLoadMatch();
 m.timer=setTimeout(()=>{if(nativeExportUI.match===m)exportLoadMatch();},180);
}
async function exportLoadMatch(game=""){
 const m=nativeExportUI.match;if(!m)return;
 clearTimeout(m.timer);m.controller?.abort();m.controller=new AbortController();
 const request=(m.request||0)+1;m.request=request;m.loading=true;m.error="";exportRenderMatch();
 try{
  const data=await fetchJSON("/api/export/match-options?"+new URLSearchParams({id:m.job.id,revision:m.job.revision,input:m.id,q:m.query,offset:m.offset,...(game?{game}:{})}),m.controller.signal);
  if(nativeExportUI.match!==m||m.request!==request)return;
  if(game){m.target=data.target;m.release="";}else{m.games=data.games;m.total=data.total;m.target=null;}
 }catch(error){if(nativeExportUI.match!==m||m.request!==request||error.name==="AbortError")return;m.error=error.message;}
 finally{if(nativeExportUI.match===m&&m.request===request){m.loading=false;exportRenderMatch();if(game&&m.target)$("export-match-release").focus();}}
}
function exportUseMatch(){
 const m=nativeExportUI.match;if(!m?.target||m.loading||m.error)return;
 const current=libraryUI.jobs.find(x=>x.id===m.job.id);
 if(!current||current.revision!==m.job.revision||!["ready","needs_review"].includes(current.state)){m.error="Plan changed. Close this selection and reopen it.";exportRenderMatch();return;}
 const d=exportIssueDraft(current);
 if(d.excluded.has(m.id)){m.error="Check this file to include it before choosing a game.";exportRenderMatch();return;}
 d.bindings.set(m.id,{id:m.id,game:m.target.game,release:m.release,game_revision:m.target.game_revision});d.excluded.delete(m.id);
 exportCloseMatch();libraryRenderOperations();
}
function exportMatchResults(m){
 const disabled=m.loading?'disabled':'';
 return (m.error?'<p class="operation-error" role="alert">'+escapeHTML(m.error)+'</p>':"")+
 (m.loading?'<p role="status">Searching…</p>':m.target?'':'<p class="secondary" role="status">'+m.total+' '+(m.total===1?'game':'games')+'</p>')+
 (m.target?'<section class="export-match-selection"><h3>'+escapeHTML(m.target.title)+'</h3><small>'+escapeHTML(m.target.game)+'</small><label>Release<select id="export-match-release" '+disabled+'><option value="">Game only — use default metadata</option>'+m.target.releases.map(x=>'<option value="'+escapeAttr(x)+'" '+(x===m.release?'selected':'')+'>'+escapeHTML(x)+'</option>').join("")+'</select></label><p class="secondary">Choose a release when you know the edition. This records your choice, not a fingerprint match.</p><div class="operation-actions"><button class="tab-button" id="export-match-back" '+disabled+'>Back to games</button><button class="catalog-primary" id="export-match-use" '+disabled+'>Use this game</button></div></section>':
 '<div class="export-match-results">'+(m.games?.length?m.games.map(x=>'<button type="button" class="export-match-result" data-export-match-game="'+escapeAttr(x.game)+'" '+disabled+'><strong>'+escapeHTML(x.name)+'</strong><small>'+escapeHTML(x.platform)+'</small><span aria-hidden="true">→</span></button>').join(""):!m.loading?'<p>No games found. Try another name.</p>':'')+'</div><div class="operation-actions"><button class="tab-button" id="export-match-prev" '+(!m.offset||m.loading?'disabled':'')+'>Previous</button><button class="tab-button" id="export-match-next" '+(m.offset+40>=m.total||m.loading?'disabled':'')+'>Next</button></div>');
}
function exportRenderMatch(){
 const m=nativeExportUI.match;if(!m)return;
 const dialog=$("export-match-dialog");dialog.classList.remove("hidden");$("app").inert=true;
 const body=exportMatchResults(m);
 if(!m.rendered){
  dialog.innerHTML='<div class="export-folder-box export-match-box"><div class="management-heading"><h2>Choose a library game</h2><button type="button" class="tab-button" id="export-match-close">Close</button></div><p class="export-match-rom">'+escapeHTML(m.rom)+'</p>'+
  '<form id="export-match-search" class="export-path-control"><input type="search" id="export-match-query" aria-label="Search games" placeholder="Search games in this platform" maxlength="256" autocomplete="off" value="'+escapeAttr(m.query)+'"></form>'+
  '<div id="export-match-body">'+body+'</div></div>';
  m.rendered=true;
  $("export-match-close").addEventListener("click",exportCloseMatch);
  $("export-match-query").addEventListener("input",()=>exportSearchMatch());
  $("export-match-search").addEventListener("submit",e=>{e.preventDefault();exportSearchMatch(true);});
  dialog.onkeydown=e=>{
   if(e.key==="Escape"){e.preventDefault();exportCloseMatch();}
   if(e.key==="Tab"){
    const controls=[...dialog.querySelectorAll("button:not(:disabled),input,select")],first=controls[0],last=controls.at(-1);
    if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}
    else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}
   }
  };
  $("export-match-query").focus();
 }else $("export-match-body").innerHTML=body;
 $("export-match-body").setAttribute("aria-busy",String(!!m.loading));
 dialog.querySelectorAll("[data-export-match-game]").forEach(b=>b.addEventListener("click",()=>exportLoadMatch(b.dataset.exportMatchGame)));
 if(!m.loading&&m.target){
  $("export-match-release").addEventListener("change",e=>m.release=e.target.value);
  $("export-match-back").addEventListener("click",()=>{m.target=null;exportRenderMatch();$("export-match-query").focus();});
  $("export-match-use").addEventListener("click",exportUseMatch);
 }else if(!m.target){
  $("export-match-prev").addEventListener("click",()=>{m.offset=Math.max(0,m.offset-40);exportLoadMatch();});
  $("export-match-next").addEventListener("click",()=>{m.offset+=40;exportLoadMatch();});
 }
}
