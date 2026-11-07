
// Editing is a mode: all inputs for the selected record appear together.
// A card revision covers every target; one Save commits all its drafts atomically.
const catalogInline={mode:false,dirty:false,saving:false,loading:false,version:0,card:null,savedCard:null,baseRevision:"",raw:null,platform:"",folder:"",target:{kind:"library",match:-1},edits:new Map(),invalid:new Map(),extra:new Set(),preferredSource:"",message:"",error:"",nameMode:"",nameDraft:""};
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
 return '<div class="'+(body?'catalog-inline-block':'fact-row catalog-inline-field')+'"><div class="catalog-inline-label"><span class="fact-label">'+cEscape(label)+'</span></div>'+(body||'<div class="fact-value">'+cEscape(value)+'</div>')+'<small class="catalog-value-origin">'+cEscape(catalogInlineOrigin(key))+'</small></div>';
}
function catalogInlineTitle(value){return cEscape(value);}
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
 // Keep the current scope visible even before its first field is changed.
 if(f.target.kind==="manual"&&!result.some(r=>catalogTargetKey(r.target)===catalogTargetKey(f.target)))result.push({target:f.target,label:(sources.find(s=>s.id===f.target.source)?.name||sourceLabel(f.target.source))+" · "+(f.target.scope||"whole game")+" · new record"});
 // Keep unsaved scoped records selectable when moving between targets.
 for(const draft of f.edits.values())if(!result.some(r=>catalogTargetKey(r.target)===catalogTargetKey(draft.target)))result.push({target:draft.target,label:sourceLabel(draft.target.source)+" · "+(draft.target.scope||"whole game")+" · draft"});
 return result;
}
function catalogScopeExists(scope){return (catalogInline.card?.records||[]).some(r=>r.target.source===catalogInline.target.source&&r.target.match>=0&&r.target.release_index==null&&!r.fields.releases&&(r.fields.release||"")===scope);}
function catalogEditorSources(){
 const f=catalogInline,ids=new Set([...(f.card?.registry?.sources||[]).map(s=>s.id),...(f.card?.records||[]).map(r=>r.target.source)]);
 return [{id:"",name:"Card fields"},...Array.from(ids,id=>({id,name:f.card.registry?.sources?.find(s=>s.id===id)?.name||sourceLabel(id)}))];
}
function catalogSelectSource(id){
 const f=catalogInline,targets=catalogEditorTargets().filter(r=>(r.target.source||"")===id);
 f.target=targets.find(r=>r.target.match>=0)?.target||targets[0]?.target||{kind:"library",match:-1};
 f.extra=new Set();f.error="";f.message="";f.nameMode="";
 if(f.card.registry.sources.some(s=>s.id===id))f.preferredSource=id;
 catalogInlineRender();
}
function catalogEditorTargetLabel(entry){
 const record=catalogInline.card?.records?.find(r=>catalogTargetKey(r.target)===catalogTargetKey(entry.target)),t=entry.target;
 if(t.kind==="library")return "Card fields";
 if(t.match===-1&&t.kind==="source")return "Service details";
 return record?.fields.release||t.scope||(t.kind==="manual"?"New whole-game entry":"Game details"+(t.match>0?" "+(t.match+1):""));
}
function catalogInlineBanner(){
 const f=catalogInline;if(!catalogEditorActive())return "";
 if(f.loading)return '<div class="catalog-inline-banner"><span role="status">Opening editable fields…</span><button class="tab-button" id="catalog-inline-cancel">Done</button></div>';
 if(!f.card)return '<div class="catalog-inline-banner"><span role="alert">'+cEscape(f.error)+'</span><button class="tab-button" id="catalog-editor-reload">Retry</button><button class="tab-button" id="catalog-inline-cancel">Done</button></div>';
 const key=catalogTargetKey(f.target),targets=catalogEditorTargets().filter(r=>(r.target.source||"")===(f.target.source||"")).filter(r=>r.target.match!==-1||r.target.kind!=="source"||Object.keys(f.card.records.find(v=>catalogTargetKey(v.target)===catalogTargetKey(r.target))?.fields||{}).length),
 source=f.card.registry?.sources?.find(s=>s.id===f.target.source),disabled=f.saving?' disabled':'',raw=state.detailTab==="raw",register=state.detailTab==="files";
 const hint=register?"Releases & fingerprints · changes are validated and saved together.":raw?"Edit the complete card YAML. Done validates and saves it.":f.target.kind==="library"?"Card fields · values saved directly in this card. Blank fields use source data.":source?'Your variant “'+source.name+'” · choose it in export priorities to use these values.':sourceLabel(f.target.source)+" · editing its saved values. Fetching this source again may replace them.";
 return '<div class="catalog-inline-banner catalog-editor-bar"><div class="catalog-editor-top"><strong>Edit card</strong><div class="catalog-inline-actions"><button class="tab-button" id="catalog-editor-discard"'+disabled+'>Discard</button><button class="catalog-primary" id="catalog-inline-save" '+(f.saving||f.invalid.size?'disabled':'')+'>'+(f.saving?'Saving…':'Done')+'</button></div></div>'+
 (!raw&&!register?'<nav class="catalog-source-tabs" aria-label="Edit source">'+catalogEditorSources().map(s=>'<button type="button" class="tab-button'+((f.target.source||"")===s.id?' active':'')+'" data-editor-source="'+escapeAttr(s.id)+'" aria-pressed="'+((f.target.source||"")===s.id)+'"'+disabled+'>'+cEscape(s.name)+'</button>').join("")+'<button class="tab-button" id="catalog-new-source" aria-label="Add named variant"'+disabled+'>+ My variant</button></nav>'+
 ((targets.length>1||source||f.target.kind==="manual")?'<div class="catalog-record-choice">'+
 (targets.length>1?'<label>Entry<select id="catalog-editor-target"'+disabled+'>'+targets.map(r=>'<option value="'+escapeAttr(catalogTargetKey(r.target))+'" '+(catalogTargetKey(r.target)===key?'selected':'')+'>'+cEscape(catalogEditorTargetLabel(r))+(f.edits.has(catalogTargetKey(r.target))?' · edited':'')+'</option>').join("")+'</select></label>':"")+
 (f.target.kind==="manual"?'<label>Applies to<select id="catalog-editor-scope"'+disabled+'><option value="" '+(catalogScopeExists("")?'disabled':'')+'>Whole game</option>'+(f.card.releases||[]).map(r=>'<option value="'+escapeAttr(r)+'" '+(f.target.scope===r?'selected':'')+' '+(catalogScopeExists(r)?'disabled':'')+'>'+cEscape(r)+'</option>').join("")+'</select></label>':"")+
 (source?'<button class="tab-button" id="catalog-rename-source"'+disabled+'>Rename</button>':"")+'</div>':""):"")+
 '<p class="catalog-edit-context">'+cEscape(hint)+'</p>'+
 (f.nameMode?'<div class="catalog-source-name"><label>Variant name<input id="catalog-source-name" maxlength="120" value="'+escapeAttr(f.nameDraft)+'"'+disabled+'></label><button class="tab-button" id="catalog-save-source-name"'+disabled+'>'+(f.nameMode==="rename"?"Save name":"Create variant")+'</button><button class="tab-button" id="catalog-cancel-source-name"'+disabled+'>Cancel</button></div>':"")+
 '<span id="catalog-editor-status" role="status">'+cEscape(f.error||f.message||(f.dirty?"Unsaved changes":""))+'</span></div>';
}
function catalogEditorEdits(){
 const f=catalogInline;return [...(f.raw!==null?[{target:{kind:"raw",match:-1},yaml:f.raw}]:[]),...f.edits.values()];
}
function catalogRenderRaw(){
 const f=catalogInline;
 return '<section class="section catalog-raw-editor"><div class="catalog-inline-actions"><h3>Card YAML</h3><button class="tab-button" id="catalog-validate-raw" '+(f.saving?'disabled':'')+'>Validate YAML</button></div><label class="sr-only" for="catalog-raw-yaml">Card YAML</label><textarea id="catalog-raw-yaml" aria-label="Card YAML" spellcheck="false" '+(f.saving?'disabled':'')+'>'+cEscape(f.raw??f.card?.yaml??"")+'</textarea></section>';
}
async function catalogMaterializeDraft(){
 const f=catalogInline;if(f.saving)return false;
 if(f.invalid.size){f.error="Correct the structured field values first.";catalogEditorActions();return false;}
 f.saving=true;f.error="";catalogInlineRender();
 try{
  const card=await catalogPost("/api/catalog/edit-preview",{platform:f.platform,game:f.folder,revision:f.baseRevision||f.card.revision,edits:catalogEditorEdits(),...catalogRegisterPayload()});
  // A preview revision belongs to the draft. Save still checks the opened revision.
  if(f.dirty){
   catalogRegisterPreview(card,true);
   f.raw=card.yaml;f.card=card;f.edits=new Map();
   if(f.target.kind==="manual")f.target=card.records?.find(r=>r.target.source===f.target.source&&r.target.match>=0&&r.target.release_index==null&&(r.fields.release||"")===(f.target.scope||""))?.target||f.target;
   if(f.target.kind==="source"&&!card.records?.some(r=>catalogTargetKey(r.target)===catalogTargetKey(f.target)))f.target={kind:"library",match:-1};
  }
  f.message=catalogRegister.loaded?"Validation passed.":"YAML is valid.";return true;
 }catch(error){f.error=error.message;return false;}
 finally{f.saving=false;catalogInlineRender();}
}
async function catalogSwitchDetailTab(tab){
 if(!catalogEditorActive()||tab===state.detailTab)return true;
 if(tab==="files"&&!await catalogEnsureRegister())return false;
 if(state.detailTab==="raw"||tab==="raw"||state.detailTab==="files")return catalogMaterializeDraft();
 return !catalogInline.saving;
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
 return '<div class="catalog-record-field" data-field-kind="'+kind+'"><label><span>'+cEscape(label)+'</span>'+control+'</label>'+(kind==="json"?'<small>Structured value · JSON</small>':kind==="list"?'<small>One value per line</small>':"")+(exists&&!(f.target.kind==="library"&&key==="title")?'<button type="button" class="catalog-reset" data-card-remove="'+escapeAttr(key)+'" title="Remove field" aria-label="Remove '+escapeAttr(label)+'"'+disabled+'>×</button>':"")+'</div>';
}
function catalogRenderEditor(response,coverHTML){
 const f=catalogInline;if(!f.card)return '<section class="game-overview">'+(f.loading?"":"<p>Reload this card to edit its fields.</p>")+'</section>';
 const fields=catalogTargetValues(),library=f.target.kind==="library",details=f.target.kind==="source"&&f.target.match===-1;
 const common=details?[]:["release_date","developer","publisher","genres",(!library&&Object.hasOwn(fields,"max_players")&&!Object.hasOwn(fields,"players")?"max_players":"players")];
 const metadata=common.map(k=>catalogEditorControl(k,fields[k])).join("");
 const descriptions=fields.descriptions&&!Array.isArray(fields.descriptions)?fields.descriptions:{},langs=Array.from(new Set([...Object.keys(descriptions),...f.extra])).sort();
 const prose=details?"":'<div class="catalog-description-fields">'+
 (Object.hasOwn(fields,"description")?catalogEditorControl("description",fields.description,"Description"):"")+
 langs.map(lang=>'<div class="catalog-record-field catalog-language-field"><label><span>Description · '+cEscape(catalogLanguages[lang]||lang)+'</span><textarea rows="9" data-card-description="'+escapeAttr(lang)+'" aria-label="Description '+escapeAttr(lang)+'" '+(f.saving?'disabled':'')+'>'+cEscape(descriptions[lang]||"")+'</textarea></label><button type="button" class="catalog-reset" data-description-remove="'+escapeAttr(lang)+'" title="Remove translation" aria-label="Remove '+escapeAttr(catalogLanguages[lang]||lang)+' translation" '+(f.saving?'disabled':'')+'>×</button></div>').join("")+
 '<details class="catalog-translation-tools" '+(!langs.length?'open':'')+'><summary>Add a translation</summary><div class="catalog-inline-actions"><input id="catalog-description-language" aria-label="Add description language" placeholder="Language code, e.g. ru"><button class="tab-button" id="catalog-add-language">Add language</button></div></details></div>';
 const omitted=new Set([...common,"title","descriptions","description",...(library?["aliases","alternate_titles"]:[])]);
 const keys=Object.keys(fields).filter(k=>!omitted.has(k));
 const extra=keys.map(k=>catalogEditorControl(k,fields[k])).join("");
 const additional='<details class="catalog-record-extra" '+(details?'open':'')+'><summary>Other fields'+(keys.length?' ('+keys.length+')':'')+'</summary><div class="catalog-record-grid">'+extra+'</div>'+
 (!library?'<div class="catalog-inline-actions"><input id="catalog-extra-field" aria-label="New source field" placeholder="Field name"><button class="tab-button" id="catalog-add-field">Add field</button></div>':"")+'</details>';
 return '<section class="game-overview catalog-record-editor"><div class="profile-layout"><div class="cover-panel">'+coverHTML+'</div>'+
 '<aside class="profile-facts catalog-editor-facts">'+metadata+'</aside><div class="profile-content">'+
 (details?"":catalogEditorControl("title",fields.title,library?"Library title":"Title"))+prose+
 (library?'<details class="catalog-record-extra"><summary>Alternative names</summary>'+catalogEditorControl("aliases",fields.aliases)+catalogEditorControl("alternate_titles",fields.alternate_titles)+'</details>':"")+additional+'</div></div></section>';
}
function catalogInitialTarget(card,key){
 if(key&&(["title","aliases","alternate_titles"].includes(key)||Object.hasOwn(card.fields||{},key)))return {kind:"library",match:-1};
 const records=(card.records||[]).filter(r=>r.target.match>=0&&r.target.release_index==null);
 const shown=state.game?.profile||{},matches=records.filter(r=>!key||Object.hasOwn(r.fields,key)||(key==="description"&&Object.hasOwn(r.fields,"descriptions")));
 const record=(key&&key!=="description"?matches.find(r=>catalogEqualValue(r.fields[key],shown[key])):null)||matches.find(r=>r.target.source===shown.source)||matches[0];
 return record?.target||{kind:"library",match:-1};
}
function catalogDiscardInline(){
 if(catalogLeaveInline(true))catalogInlineRender();
}
async function catalogFinishInline(){
 const f=catalogInline;if(f.saving||f.invalid.size)return;
 const version=f.version;
 if(f.dirty&&!await catalogSaveInline())return;
 if(f.version===version&&catalogLeaveInline())catalogInlineRender();
}
function catalogEqualValue(a,b){
 const canonical=v=>Array.isArray(v)?v.map(canonical):v&&typeof v==="object"?Object.fromEntries(Object.keys(v).sort().map(k=>[k,canonical(v[k])])):v;
 return JSON.stringify(canonical(a))===JSON.stringify(canonical(b));
}
function catalogEditorChange(key,value){
 if(catalogInline.saving)return;
 const f=catalogInline,token=catalogTargetKey(f.target),draft=f.edits.get(token)||{target:{...f.target},fields:Object.create(null)},base=catalogTargetBase();
 if(catalogEqualValue(value,base[key])||(value==null&&!Object.hasOwn(base,key)))delete draft.fields[key];else draft.fields[key]=value;
 if(Object.keys(draft.fields).length)f.edits.set(token,draft);else f.edits.delete(token);
 f.dirty=f.raw!==null||f.edits.size>0||catalogRegisterDirty();f.error="";f.message="";catalogEditorActions();
}
function catalogEditorDescription(lang,value){
 const descriptions={...(catalogTargetValues().descriptions||{})};
 if(value.trim())descriptions[lang]=value;else delete descriptions[lang];
 catalogEditorChange("descriptions",Object.keys(descriptions).length?descriptions:null);
}
function catalogRemoveDescription(lang){
 if(catalogInline.saving)return;
 catalogInline.extra.delete(lang);catalogEditorDescription(lang,"");catalogInlineRender();
}
function catalogEditorActions(){
 const f=catalogInline;
 if($("catalog-inline-save"))$("catalog-inline-save").disabled=f.saving||f.invalid.size>0;
 if($("catalog-editor-discard"))$("catalog-editor-discard").disabled=f.saving;
 if($("catalog-editor-count"))$("catalog-editor-count").textContent=f.edits.size+" changed "+(f.edits.size===1?"record":"records");
 if($("catalog-editor-status"))$("catalog-editor-status").textContent=f.error||f.message||(f.invalid.size?"Check structured field values":f.dirty?"Unsaved changes":"");
}
function catalogInlineRender(){const scroll=$("detail-body").scrollTop;renderDetails();$("detail-body").scrollTop=scroll;catalogUpdateTools();}
function catalogLeaveInline(discard=false){
 const f=catalogInline;if(f.saving)return false;
 if(!discard&&(f.dirty||f.invalid.size)&&!window.confirm("Discard unsaved card changes?"))return false;
 catalogRegisterInstall(null,true);
 f.version++;f.mode=false;f.loading=false;f.dirty=false;f.edits=new Map();f.invalid=new Map();f.extra=new Set();f.card=null;f.savedCard=null;f.raw=null;f.baseRevision="";f.error="";f.message="";f.nameMode="";return true;
}
async function catalogEditCard(){
 if(!catalogInlineAvailable()||catalogUI.busy)return;
 if(catalogInline.mode){await catalogFinishInline();return;}
 await catalogStartInline();
}
async function catalogStartInline(key){
 const f=catalogInline;if(!catalogInlineAvailable()||catalogUI.busy)return;
 if(f.mode&&f.card&&catalogInlineCurrent()){if(key)document.querySelectorAll("[data-card-field]").forEach(e=>{if(e.dataset.cardField===key)e.focus?.();});return;}
 const platform=state.platform,folder=state.selectedFolder,version=++f.version,openRegister=!key&&state.detailTab==="files";
 catalogRegisterInstall(null,true);
 Object.assign(f,{mode:true,loading:true,platform,folder,error:"",message:""});state.detailTab="preview";catalogInlineRender();
 try{
  const card=await fetchJSON("/api/catalog/edit?"+new URLSearchParams({platform,game:folder}));
  if(version!==f.version||platform!==state.platform||folder!==state.selectedFolder)return;
  if(card.editor_error)throw new Error(card.editor_error);
  f.card=card;f.savedCard=card;f.baseRevision=card.revision;f.raw=null;f.dirty=false;f.target=catalogInitialTarget(card,key);f.edits=new Map();f.invalid=new Map();f.extra=new Set();
  if(!key&&f.preferredSource&&card.registry?.sources?.some(s=>s.id===f.preferredSource)){
   f.target=card.records?.find(r=>r.target.source===f.preferredSource&&r.target.match>=0&&r.target.release_index==null)?.target||{kind:"manual",source:f.preferredSource,match:-1,scope:""};
  }
  if(openRegister){f.loading=false;if(await catalogEnsureRegister())state.detailTab="files";}
  if(state.sourceDefaults)state.sourceDefaults.source_labels={...(state.sourceDefaults.source_labels||{}),...Object.fromEntries((card.registry?.sources||[]).map(s=>[s.id,s.name]))};
 }catch(error){if(version===f.version)f.error=error.message;}
 finally{if(version===f.version){f.loading=false;catalogInlineRender();if(key)document.querySelectorAll("[data-card-field]").forEach(e=>{if(e.dataset.cardField===key)e.focus?.();});}}
}
async function catalogSaveInline(){
 const f=catalogInline;if(f.saving||!f.dirty||f.invalid.size||!catalogInlineCurrent())return;
 let saved=false;const platform=f.platform,folder=f.folder,target=f.target;f.saving=true;catalogUI.busy=true;f.error="";catalogInlineRender();
 try{
  const response=await catalogPost("/api/catalog/edit",{platform,game:folder,revision:f.baseRevision||f.card.revision,edits:catalogEditorEdits(),...catalogRegisterPayload()});
  catalogRegisterInstall(response.card.register||null,true);
  if(typeof resetFingerprints==="function")resetFingerprints();
  f.card=response.card;f.savedCard=response.card;f.baseRevision=response.card.revision;f.raw=null;f.edits=new Map();f.invalid=new Map();f.dirty=false;f.message=response.warning||"Saved.";
  if(target.kind==="manual")f.target=f.card.records.filter(r=>r.target.source===target.source&&r.target.match>=0&&r.target.release_index==null).find(r=>(r.fields.release||"")===(target.scope||""))?.target||{kind:"library",match:-1};
  if(state.platform===platform&&state.selectedFolder===folder){
   state.game.profile=response.card.profile;state.game.catalog_fields=response.card.fields;state.game.catalog_yaml=response.card.yaml;
   if(state.game.game){state.game.game.title=response.card.fields.title;state.game.game.aliases=response.card.fields.aliases||[];}
   const row=state.games.find(g=>g.folder===folder);if(row){row.title=response.card.fields.title;if(response.card.register){row.release_count=response.card.register.releases.length;row.default_release=response.card.register.default_release;}}
   renderGames();if(typeof resetPreviews==="function")resetPreviews();
   try{
    const fresh=await fetchJSON("/api/game?"+new URLSearchParams({platform,game:folder}));
    if(fresh?.game){state.game=fresh;if(row){row.release_count=fresh.game.releases?.length||0;row.default_release=fresh.game.default_release||"";row.variant_count=fresh.game.variants?.length||0;renderGames();}}
    state.sourceDefaults=await fetchJSON("/api/export-options");
    if(typeof renderSourceControls==="function")renderSourceControls();
   }catch(error){f.message+=" Could not refresh: "+error.message;}
  }
  catalogUI.cardsCache=new Map();saved=true;
 }catch(error){f.error=error.message;}
 finally{f.saving=false;catalogUI.busy=false;catalogInlineRender();}
 return saved;
}
async function catalogSaveSourceName(){
 const f=catalogInline;if(f.saving)return;
 const name=$("catalog-source-name").value,id=f.nameMode==="rename"?f.target.source:"";
 f.nameDraft=name;f.saving=true;catalogUI.busy=true;f.error="";catalogInlineRender();
 try{
  const response=await catalogPost("/api/catalog/curation",{revision:f.card.registry.revision,id,name});
  f.card.registry=response.registry;if(f.savedCard)f.savedCard.registry=response.registry;f.nameMode="";f.preferredSource=response.source.id;
  if(!id){f.target={kind:"manual",source:response.source.id,match:-1,scope:""};f.extra=new Set();}
  f.message=id?"Variant renamed. Saved profiles keep the same source.":"Variant created. Add its fields, then Done; enable it in your metadata priorities.";
  if(state.sourceDefaults)state.sourceDefaults.source_labels={...(state.sourceDefaults.source_labels||{}),[response.source.id]:response.source.name};
  try{state.sourceDefaults=await fetchJSON("/api/export-options");if(typeof renderSourceControls==="function")renderSourceControls();}catch(error){f.message+=" Could not refresh source choices: "+error.message;}
  if(typeof librarySettingsUI!=="undefined")librarySettingsUI.sections=null;
 }catch(error){f.error=error.message;}
 finally{f.saving=false;catalogUI.busy=false;catalogInlineRender();}
}
function catalogBindInline(){
 if(!catalogEditorActive())return;
 catalogBindRegister();
 const f=catalogInline;
 document.querySelectorAll("[data-editor-source]").forEach(el=>el.addEventListener("click",()=>catalogSelectSource(el.dataset.editorSource)));
 $("catalog-raw-yaml")?.addEventListener("input",e=>{if(f.saving)return;f.raw=e.target.value;f.dirty=true;f.error="";f.message="";catalogEditorActions();});
 $("catalog-validate-raw")?.addEventListener("click",catalogMaterializeDraft);
 $("catalog-editor-reload")?.addEventListener("click",()=>{f.mode=false;catalogStartInline();});
 $("catalog-editor-target")?.addEventListener("change",e=>{f.target=JSON.parse(e.target.value);f.extra=new Set();f.error="";f.message="";f.nameMode="";if(f.card.registry.sources.some(s=>s.id===f.target.source))f.preferredSource=f.target.source;catalogInlineRender();});
 $("catalog-editor-scope")?.addEventListener("change",e=>{f.target={...f.target,scope:e.target.value};f.extra=new Set();catalogInlineRender();});
 $("catalog-new-source")?.addEventListener("click",()=>{f.nameMode="create";f.nameDraft="";catalogInlineRender();$("catalog-source-name")?.focus?.();});
 $("catalog-rename-source")?.addEventListener("click",()=>{f.nameMode="rename";f.nameDraft=f.card.registry.sources.find(s=>s.id===f.target.source)?.name||"";catalogInlineRender();$("catalog-source-name")?.focus?.();});
 $("catalog-source-name")?.addEventListener("input",e=>{f.nameDraft=e.target.value;});
 $("catalog-save-source-name")?.addEventListener("click",catalogSaveSourceName);
 $("catalog-cancel-source-name")?.addEventListener("click",()=>{f.nameMode="";catalogInlineRender();});
 $("catalog-inline-save")?.addEventListener("click",catalogFinishInline);
 $("catalog-inline-cancel")?.addEventListener("click",()=>{if(catalogLeaveInline())catalogInlineRender();});
 $("catalog-editor-discard")?.addEventListener("click",catalogDiscardInline);
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
 document.querySelectorAll("[data-description-remove]").forEach(button=>button.addEventListener("click",()=>catalogRemoveDescription(button.dataset.descriptionRemove)));
 document.querySelectorAll("[data-card-remove]").forEach(button=>button.addEventListener("click",()=>{if(f.saving)return;f.invalid.delete(catalogTargetKey(f.target)+"|"+button.dataset.cardRemove);catalogEditorChange(button.dataset.cardRemove,null);catalogInlineRender();}));
 $("catalog-add-language")?.addEventListener("click",()=>{if(f.saving)return;const lang=$("catalog-description-language").value.trim().toLowerCase();if(!/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/.test(lang)){f.error="Use a language code such as en, ru or pt-BR.";catalogEditorActions();return;}f.extra.add(lang);catalogInlineRender();});
 $("catalog-add-field")?.addEventListener("click",()=>{if(f.saving)return;const key=$("catalog-extra-field").value.trim();if(!key||["matches","__proto__","constructor","prototype"].includes(key)||Object.hasOwn(catalogTargetValues(),key))return;catalogEditorChange(key,"");catalogInlineRender();});
}
window.addEventListener?.("beforeunload",event=>{if(catalogInline.dirty||catalogInline.invalid.size||catalogInline.saving){event.preventDefault();event.returnValue="";}});

catalogCapabilities();
