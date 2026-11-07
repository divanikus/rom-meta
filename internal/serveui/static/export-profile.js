
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
