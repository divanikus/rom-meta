package dat

import "strings"

// Hardware names and shared-driver splits reviewed against the official mame0289
// source. These are board families, not ROM compatibility claims. Keep mixed
// drivers scoped to known sets; a new set must not inherit a guessed board.
func extendArcadeHardware(r *ArcadeRouting) {
	r.SourceSets = map[string]map[string]string{}
	platform := func(name, source string) string {
		name = "Arcade - " + name
		if _, exists := r.Platforms[name]; !exists {
			r.Platforms[name] = ArcadePlatform{
				Kind:           "hardware",
				NamingEvidence: "RomMeta formatted MAME board family; https://github.com/mamedev/mame/blob/mame0289/src/mame/" + source,
			}
		}
		return name
	}
	driver := func(name, source string) {
		r.Sources[source] = platform(name, source)
	}
	bios := func(name, source, names string) {
		p := platform(name, source)
		for _, id := range strings.Fields(names) {
			r.BIOS[id] = p
		}
	}
	sets := func(name, source, names string) {
		p := platform(name, source)
		if r.SourceSets[source] == nil {
			r.SourceSets[source] = map[string]string{}
		}
		for _, id := range strings.Fields(names) {
			r.SourceSets[source][id] = p
		}
	}
	driver("IGS - PGM", "igs/pgm.cpp")
	driver("IGS - PGM 2", "igs/pgm2.cpp")
	driver("Sega - System 16A", "sega/segas16a.cpp")
	driver("Sega - System 18", "sega/segas18.cpp")
	driver("Sega - System 24", "sega/segas24.cpp")
	driver("Sega - ST-V", "sega/stv.cpp")
	driver("Sega - Hang-On", "sega/segahang.cpp")
	driver("Sega - Out Run", "sega/segaorun.cpp")
	driver("Sega - X Board", "sega/segaxbd.cpp")
	driver("Sega - Y Board", "sega/segaybd.cpp")
	driver("Sega - Model 1", "sega/model1.cpp")
	driver("Sega - Chihiro", "sega/chihiro.cpp")
	driver("Sega - Triforce", "sega/triforce.cpp")
	driver("Sega - Lindbergh", "sega/lindbergh.cpp")
	driver("Sega - System SP", "sega/segasp.cpp")
	driver("Sega - System E", "sega/segae.cpp")
	driver("Konami - System 573", "konami/ksys573.cpp")
	driver("Konami - Hornet", "konami/hornet.cpp")
	driver("Konami - System GX", "konami/konamigx.cpp")
	driver("Konami - System GV", "konami/konamigv.cpp")
	driver("Konami - M2", "konami/konamim2.cpp")
	driver("Konami - Python 2", "konami/kpython2.cpp")
	driver("Konami - Viper", "konami/viper.cpp")
	driver("Konami - NWK-TR", "konami/nwk-tr.cpp")
	driver("Konami - Firebeat", "konami/firebeat.cpp")
	driver("Konami - Twinkle", "konami/twinkle.cpp")
	driver("Konami - Twin16", "konami/twin16.cpp")
	driver("Konami - ZR107", "konami/zr107.cpp")
	driver("Konami - GTI Club", "konami/gticlub.cpp")
	driver("Konami - DJ Main", "konami/djmain.cpp")
	driver("Taito - F2", "taito/taito_f2.cpp")
	driver("Taito - F3", "taito/taito_f3.cpp")
	driver("Taito - Z System", "taito/taito_z.cpp")
	driver("Taito - B System", "taito/taito_b.cpp")
	driver("Taito - L System", "taito/taito_l.cpp")
	driver("Taito - X System", "taito/taito_x.cpp")
	driver("Taito - H System", "taito/taito_h.cpp")
	driver("Taito - SJ System", "taito/taitosj.cpp")
	driver("Taito - JC System", "taito/taitojc.cpp")
	driver("Taito - Type Zero", "taito/taitotz.cpp")
	// The skeleton uses one machine configuration for multiple PC generations.
	sets("Taito - Type X", "taito/taitotx.cpp", "chaosbrk gwinggen homura hotgmkmp raiden3 spicaadv usagiol shikiga3 raiden4 kof98um trbwtchs goketsuj kofskyst")
	sets("Taito - Type X2", "taito/taitotx.cpp", "chasehq2 samspsen kofxii")
	driver("Taito - G-NET", "sony/taitogn.cpp")
	driver("Namco - System 1", "namco/namcos1.cpp")
	driver("Namco - System 2", "namco/namcos2.cpp")
	driver("Namco - System 10", "namco/namcos10.cpp")
	driver("Namco - System 11", "namco/namcos11.cpp")
	driver("Namco - System 12", "namco/namcos12.cpp")
	driver("Namco - System 86", "namco/namcos86.cpp")
	driver("Midway - T-Unit", "williams/midtunit.cpp")
	driver("Midway - Wolf Unit", "williams/midwunit.cpp")
	driver("Midway - Vegas", "williams/vegas.cpp")
	driver("Midway - Killer Instinct", "rare/kinst.cpp")
	driver("Atari - System 1", "atari/atarisy1.cpp")
	driver("Atari - System 2", "atari/atarisy2.cpp")
	driver("Atari - G1", "atari/atarig1.cpp")
	driver("Atari - GX2", "atari/atarigx2.cpp")
	driver("Jaleco - Mega System 1", "jaleco/megasys1.cpp")
	driver("Jaleco - Mega System 32", "jaleco/ms32.cpp")
	driver("Kaneko - Super Nova System", "kaneko/suprnova.cpp")
	driver("Seta - SSV", "seta/ssv.cpp")
	driver("Data East - DECO Cassette System", "dataeast/decocass.cpp")
	driver("Data East - MLC", "dataeast/deco_mlc.cpp")
	driver("Irem - M62", "irem/m62.cpp")
	driver("Irem - M90", "irem/m90.cpp")
	driver("Irem - M92", "irem/m92.cpp")
	driver("Cave - CV1000", "cave/cv1k.cpp")
	driver("SNK - Hyper Neo Geo 64", "snk/hng64.cpp")
	driver("Nintendo - Super System", "nintendo/nss.cpp")
	driver("Arcadia Systems - Arcadia", "amiga/arsystems.cpp")
	driver("Century - CVS", "misc/cvs.cpp")
	bios("Sega - Naomi", "sega/naomi.cpp", "airlbios f355dlx f355bios hod2bios")
	bios("Namco - System 246", "namco/namcops2.cpp", "sys246")
	bios("Namco - System 256", "namco/namcops2.cpp", "sys256")
	bios("Capcom - ZN-1", "sony/zn.cpp", "coh1000c")
	bios("Capcom - ZN-2", "sony/zn.cpp", "coh3002c")
	bios("Taito - FX-1", "sony/zn.cpp", "coh1000t")
	bios("Acclaim - ZN-1", "sony/zn.cpp", "coh1000a")
	bios("Time Warner - ZN-1", "sony/zn.cpp", "coh1000w")
	bios("Atlus - ZN-1", "sony/zn.cpp", "coh1001l")
	bios("Video System - ZN-1", "sony/zn.cpp", "coh1002v")
	bios("Eighting-Raizing - ZN-1", "sony/zn.cpp", "coh1002e")
	bios("Tecmo - TPS", "sony/zn.cpp", "coh1002m")
	bios("Seta - Aleck64", "nintendo/aleck64.cpp", "aleck64")
	sets("Namco - Super System 256", "namco/namcops2.cpp", "timecrs4 timecrs4j")
	sets("Seta - E90", "nintendo/aleck64.cpp", "mtetrisc")
	sets("Atari - CoJag", "atari/jaguar.cpp", `
		a51mxr3k a51mxr3ka area51 area51a area51mx area51t area51ta fishfren freezeat freezeat2 freezeat3
		freezeat4 freezeat5 freezeat6 freezeatjp maxf_102 maxf_ng maxforce vcircle
	`)
	sets("Sega - System 32", "sega/segas32.cpp", `
		alien3 alien3j alien3u arabfgt arabfgtj arabfgtu arescue arescuej arescueu brival brivalj darkedge
		darkedgej dbzvrvs f1en f1enj f1enu f1lap f1lapj f1lapt ga2 ga2j ga2u holo jleague jleagueo jpark jparkj
		jparkja jparkjc kokoroj kokoroj2 kokoroja radm radmu radr radrj radru slipstrm slipstrmh sonic sonicp
		spidman spidmanj spidmanu svf svfo svs
	`)
	sets("Sega - Multi 32", "sega/segas32.cpp", "as1 as1a as1b as1c harddunk harddunkj orunners orunnersj orunnersu scross scrossa scrossu titlef titlefj titlefu")
	sets("Sega - System C", "sega/segac2.cpp", "bloxeedc bloxeedu column2j columns columns2 columnsj columnsu ichirjbl tantrbl2 tantrbl3")
	sets("Sega - System C2", "sega/segac2.cpp", `
		anpanman anpanmana borench borencha borenchj headonch ichir ichirbl ichirj ichirk ooparts pclub pclubj
		pclubjv2 pclubjv3 pclubjv4 pclubjv5 potopoto puyo puyobl puyoj puyoja puyopuy2 ribbit ribbitj soniccar
		sonicfgt sonicfgtj sonicpop ssonicbr stkclmns stkclmnsj tantr tantrbl tantrbl4 tantrkor tfrceac
		tfrceacb tfrceacj tfrceacjpb twinsqua wwanpanm wwanpanmo wwjumbo wwmarine wwpajero zunkyou
	`)
	sets("Sega - System 1", "sega/system1.cpp", `
		4dwarrio blockgal brain bullfgt flicky flickya flickyb flickyo flickys1 flickys2 flickyup flickyupa
		gardia hvymetal imsorry imsorryj mrviking mrvikingj myhero myherobl myherok ninja nob nobb nprinces
		nprincesb nprinceso nprincesu pitfall2 pitfall2a pitfall2u raflesia raflesiau regulus reguluso regulusu
		seganinj seganinja seganinju spatter spattera ssanchan sscandal starjack starjacks swat teddybb
		teddybbo teddybboa teddybbobl thetogyu upndown upndownu wbdeluxe wboy wboy2 wboy2u wboy3 wboy4 wboy5
		wboy6 wboyblt wboyo wboyu wboyub wmatch
	`)
	sets("Sega - System 2", "sega/system1.cpp", `
		blockgalb choplift chopliftbl chopliftu dakkochn gardiab gardiaj shtngmst tokisens tokisensa ufosensi
		ufosensib wbml wbmlb wbmlb2 wbmlbg wbmlbge wbmld wbmlh wbmljb wbmljo wbmljod wbmlvc wbmlvcd wboysys2
		wboysys2a
	`)
	sets("Sega - System 16B", "sega/segas16b.cpp", `
		aceattac afightere afighterf afighterg afighterh aliensyn aliensyn3 aliensyn7 aliensynj altbeast
		altbeast2 altbeast4 altbeast5 altbeast5d altbeast6 altbeastj altbeastj1 altbeastj3 altbeastj3d aurail
		aurail1 aurail1d aurailj aurailjd bayroute bayroute1 bayrouted bayroutej bayroutejd bullet bulletd
		cencourt cotton cottond cottonj cottonja cottonjad cottonjd cottonu cottonud dddoor ddux ddux1 dduxd
		dduxj dduxjd defense dunkshot dunkshota dunkshoto eswat eswatd eswatj eswatj1 eswatj1d eswatjd eswatu
		eswatud exctleag exctleagd fantzoneta fpoint fpoint1 fpoint1d fpointd goldnaxe goldnaxe1 goldnaxe1d
		goldnaxe2 goldnaxe3 goldnaxe3d goldnaxej goldnaxejd goldnaxeu goldnaxeud hwchamp hwchampa hwchampj
		hwchampjd mvp mvpd mvpj mvpjd passsht passshta passshtad passshtd passshtj passshtjd riotcity ryukyu
		ryukyua ryukyud sdib sdibl sdibl2 sdibl3 sdibl4 sdibl5 sdibl6 shinobi2 shinobi2d shinobi3 shinobi4
		shinobi5 shinobi6 sjryuko sonicbom sonicbomd suprleag tetris1 tetris1d tetris2 tetris2d timescan
		timescan3 toryumon tturf tturfu ultracin wb3 wb32 wb32d wb33 wb33d wb34 wb34d wfishing wrestwar
		wrestwar1 wrestwar1d wrestwar2 wrestwar2d wwthomas
	`)
	sets("Sega - System 16C", "sega/segas16b.cpp", "fantzn2x fantzn2xp")
	sets("Namco - System 22", "namco/namcos22.cpp", `
		acedrive cybrcomm raverace raveracej raveraceja ridgera2 ridgera28 ridgera2j ridgera2ja ridgerac
		ridgerac3m ridgeraca ridgeracb ridgeracc ridgeracf ridgeracj victlap victlapa victlapj
	`)
	sets("Namco - Super System 22", "namco/namcos22.cpp", `
		adillor adillorj airco22b alpinerc alpinerd alpinerjc alpines alpinr2a alpinr2b aquajet cybrcycc
		cybrcyccj dirtdash dirtdasha dirtdashb dirtdashj propcycl propcyclj timecris timecrisa timecrisj
		tokyowar tokyowarj
	`)
	sets("Namco - System 22.5", "namco/namcos23.cpp", "finfurl rapidrvr rapidrvrp rapidrvrv2c")
	sets("Namco - System 23", "namco/namcos23.cpp", "downhill downhillu motoxgo motoxgov1a motoxgov2a panicprk panicprkj timecrs2 timecrs2v1b timecrs2v2b")
	sets("Namco - Super System 23", "namco/namcos23.cpp", "500gp aking finfurl2 finfurl2j gunwars gunwarsa raceon raceonj timecrs2v4a timecrs2v5a")
	sets("Namco - System 23 Evolution 2", "namco/namcos23.cpp", "crszone crszonev2a crszonev2b crszonev3a crszonev3b crszonev3b2 crszonev4a")
	sets("Namco - NA-1", "namco/namcona1.cpp", "bkrtmaq cgangpzl cgangpzlj emeraldaj emeraldajb exvania exvaniaj fa fghtatck swcourt swcourtb swcourtj tinklpit")
	sets("Namco - NA-2", "namco/namcona1.cpp", "bbbingo bbbingot emeralda emeraldajd knckhead knckheadj knckheadjp numanath numanathj quiztou xday2 zelos zelost")
	sets("Namco - NB-1", "namco/namconb1.cpp", `
		gslgr94j gslgr94u gslgr94ua gslugrsj gunbuletj gunbuletw nebulray nebulrayj nebulrayp ptblank ptblanka
		sws95 sws96 sws97 vshoot
	`)
	sets("Namco - NB-2", "namco/namconb1.cpp", "machbrkr machbrkrj outfxies outfxiesa outfxiesj outfxiesja")
	sets("Midway - Z-Unit", "williams/midyunit.cpp", "narc narc1 narc2 narc3 narc4 narc6")
	sets("Midway - Y-Unit", "williams/midyunit.cpp", `
		hiimpact hiimpact1 hiimpact2 hiimpact3 hiimpact4 hiimpactp mazebl mkla1 mkla2 mkla3 mkla3bl mkla4
		mknifty mknifty666 mkprot4 mkprot8 mkprot9 mkrep mkyturbo mkyturboe saurnfrnt shimpact shimpactl1
		shimpactp4 shimpactp5 shimpactp6 smashtv smashtv3 smashtv4 smashtv5 smashtv6 strkforc term2 term2la1
		term2la2 term2la3 term2lg1 term2pa2 totcarn totcarnp1 totcarnp2 trog trog3 trog3a trog4 trogpa4 trogpa5
		trogpa6 yunittst
	`)
	sets("Midway - V-Unit", "williams/midvunit.cpp", `
		crusnusa crusnusa11 crusnusa20 crusnusa21 crusnusa40 crusnusa41 crusnusa44 crusnwld crusnwld13
		crusnwld17 crusnwld19 crusnwld20 crusnwld23 crusnwld24 offroadc offroadc0 offroadc1 offroadc3 offroadc4
		offroadc5
	`)
	sets("Midway - V-Unit Plus", "williams/midvunit.cpp", "wargods wargodsa wargodsb")
	sets("Midway - Zeus", "williams/midzeus.cpp", "invasnab invasnab3 invasnab4 mk4 mk4a mk4b")
	sets("Midway - Zeus II", "williams/midzeus.cpp", "crusnexo crusnexoa crusnexoaa crusnexob crusnexoc crusnexod crusnexoe thegrid thegrida thegridb")
	sets("Atari - Phoenix", "williams/seattle.cpp", "wg3dh wg3dhfe")
	sets("Midway - Seattle", "williams/seattle.cpp", `
		biofreak blitz blitz2k blitz99 blitz99a calspeed calspeeda calspeedb carnevil carnevil1 hyprdriv
		hyprdriv131 mace macea vaportrx vaportrxp
	`)
	sets("Atari - Flagstaff", "williams/seattle.cpp", "sfrush sfrushrk sfrushrkw sfrushrkwo")
	sets("Seibu Kaihatsu - SPI", "seibu/seibuspi.cpp", `
		batlball batlballa batlballe batlballo batlballpt batlballu ejanhs rdft rdft2 rdft2a rdft2aa rdft2it
		rdft2j rdft2ja rdft2jb rdft2jc rdft2s rdft2t rdft2u rdfta rdftadi rdftadia rdftam rdftau rdftauge
		rdftgb rdftgr rdftit rdftj rdftja rdftjb rdftu rdftua rfjet rfjeta rfjetj rfjett rfjetu senkyu senkyua
		viprp1 viprp1h viprp1hk viprp1j viprp1k viprp1oj viprp1ot viprp1pt viprp1s viprp1t viprp1u viprp1ua
	`)
	sets("Seibu Kaihatsu - SXX2E", "seibu/seibuspi.cpp", "rdfts")
	sets("Seibu Kaihatsu - SXX2F", "seibu/seibuspi.cpp", "rdft2us")
	sets("Seibu Kaihatsu - SXX2G", "seibu/seibuspi.cpp", "rfjets rfjetsa")
	sets("Seibu Kaihatsu - SYS386I", "seibu/seibuspi.cpp", "rdft22kc rfjet2kc")
	sets("Seibu Kaihatsu - SYS386F", "seibu/seibuspi.cpp", "ejsakura ejsakura12")
	sets("Irem - M72", "irem/m72.cpp", `
		airdueljm72 airduelm72 bchopper dbreedjm72 dbreedm72 dkgensanm72 gallopm72 imgfight imgfightj
		imgfightjb loht lohtb2 lohtb3 lohtj mrheli nspirit nspiritj rtype rtypeb rtypej rtypejp rtypeu
		xmultiplm72
	`)
	sets("Irem - M81", "irem/m72.cpp", "dbreed hharry xmultipl")
	sets("Irem - M82", "irem/m72.cpp", "airduel airduelu dkgensanm82 majtitle majtitlej rtype2m82b rtypem82b")
	sets("Irem - M84", "irem/m72.cpp", "cosmccop dkgensan dkgensana gallop hharryb hharryu kengo kengoj ltswords rtype2 rtype2j rtype2jc")
	sets("Irem - M85", "irem/m72.cpp", "poundfor poundforj poundforu")
	sets("Psikyo - PS3-V1", "psikyo/psikyosh.cpp", "daraku s1945ii sbomber sbombera soldivid soldividk")
	sets("Psikyo - PS5", "psikyo/psikyosh.cpp", "dragnblz gnbarich gunbird2 gunbird2a mjgtaste s1945iii tgm2 tgm2p")
}
