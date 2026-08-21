# -*- coding: utf-8 -*-
"""Tidal Release — recruit poster generator (v2, refined)"""
from PIL import Image, ImageDraw, ImageFont
import math, random

W, H = 2480, 3508
IMG = Image.new("RGB", (W, H), (238, 232, 221))
D = ImageDraw.Draw(IMG)

# ---------- palette ----------
PAPER   = (238, 232, 221)
PAPER2  = (247, 243, 234)
INK     = (18, 33, 42)
INK2    = (52, 72, 84)
INKSOFT = (130, 138, 140)
SEA_DP  = (16, 38, 50)
SEA_MD  = (30, 68, 86)
SEA_LT  = (48, 99, 116)
SAND    = (201, 168, 106)
RUST    = (176, 88, 58)
GRID    = (206, 197, 181)
DGRID   = (74, 114, 128)
ANCHOR  = (188, 72, 52)
CREAM   = (242, 237, 227)
SUN     = (235, 213, 165)
SUN2    = (206, 178, 126)

# ---------- fonts ----------
F_DIR = "/Users/lisun/workplace/go-harness/web/skills/canvas-design/canvas-fonts"
def F(p, s): return ImageFont.truetype(p, int(s))

ZH_TITLE = "/System/Library/Fonts/Hiragino Sans GB.ttc"
ZH_BODY  = "/System/Library/Fonts/AppleSDGothicNeo.ttc"
ZH_SONG  = "/System/Library/Fonts/Songti.ttc"

f_tit   = F(ZH_TITLE, 272)
f_ser   = F(F_DIR+"/InstrumentSerif-Italic.ttf", 74)
f_monoB = F(F_DIR+"/GeistMono-Bold.ttf", 38)
f_monoD = F(F_DIR+"/DMMono-Regular.ttf", 32)
f_monoC = F(F_DIR+"/GeistMono-Regular.ttf", 33)
f_zhM   = F(ZH_BODY, 44)
f_zhS   = F(ZH_BODY, 38)
f_song  = F(ZH_SONG, 62)

# ---------- geometry ----------
ML, MR = 168, 168
W_CEN = W/2
SEA_Y = 2250
BOT   = H - 150

# ---------- helpers ----------
def tw(s, f):
    b = D.textbbox((0,0), s, font=f); return b[2]-b[0]
def tracked_cx(draw, cx, y, s, f, fill, tracking):
    widths=[tw(c,f) for c in s]
    total=sum(widths)+tracking*(len(s)-1)
    x=cx-total/2
    for i,c in enumerate(s):
        draw.text((x,y),c,font=f,fill=fill); x+=widths[i]+tracking
    return total

# ---------- base grid (survey paper) ----------
for gx in range(0, W+1, 130):
    D.line([(gx,0),(gx,H)], fill=GRID, width=1)
for gy in range(0, H+1, 130):
    D.line([(0,gy),(W,gy)], fill=GRID, width=1)

# ---------- sky: subtle stratospherics above horizon ----------
for k in range(4):
    y = SEA_Y - 260 - k*150
    wv = 420 - k*70
    D.line([(W_CEN-14, y),(W_CEN-wv, y-3)], fill=(224,210,186), width=2)
    D.line([(W_CEN+14, y),(W_CEN+wv, y-3)], fill=(224,210,186), width=2)

# ---------- sea body gradient ----------
base=SEA_MD; deep=SEA_DP
for y in range(int(SEA_Y), BOT+1):
    t=(y-SEA_Y)/(BOT-SEA_Y)
    D.line([(0,y),(W,y)], fill=tuple(int(base[i]*(1-t)+deep[i]*t) for i in range(3)))
# sea fine grid
for gx in range(48, W, 78):
    D.line([(gx,SEA_Y),(gx,BOT)], fill=DGRID, width=1)
for gy in range(int(SEA_Y+44), BOT, 78):
    D.line([(40,gy),(W-40,gy)], fill=DGRID, width=1)

# ---------- bathymetric contour lines ----------
def cy_f(i): return SEA_Y + 20 + i*56 + i*i*1.9
contours=[]; i=0
while cy_f(i) < BOT-24:
    contours.append(cy_f(i)); i+=1
x0, x1 = 44, W-44
for idx, cY in enumerate(contours):
    amp = 15 + idx*1.6
    col = (126,170,184) if idx%2==0 else (100,151,169)
    pts=[]
    st=2
    for sx in range(x0, x1+st, st):
        off = math.sin((sx/(66+idx*9))*math.pi*2)*amp
        off += math.sin((sx/17))*amp*0.25
        pts.append((sx, cY+off*0.45))
    D.line(pts, fill=col, width=2, joint="curve")
    depth = int(round((idx+1)*1.6*1.13**idx))
    lab = f"-{depth}"
    D.text((x1-tw(lab,f_monoD)-86, cY-20), lab, font=f_monoD, fill=CREAM)
    D.line([(x1-70, cY),(x1-16, cY)], fill=CREAM, width=2)
    D.line([(x0+4, cY),(x0+26, cY)], fill=CREAM, width=2)

# ---------- horizon line ----------
D.line([(ML, SEA_Y),(W-MR, SEA_Y)], fill=(242,236,222), width=3)
D.line([(ML, SEA_Y-9),(W-MR, SEA_Y-9)], fill=(198,180,152), width=1)
# sea+sky divider tick notches
for gx in range(ML, W-MR+1, 130):
    D.line([(gx, SEA_Y-6),(gx, SEA_Y+6)], fill=(170,158,136), width=2)

# ---------- reference anchor: YOU ARE HERE ----------
ax, ay = W/2, 400
D.ellipse([ax-15,ay-15,ax+15,ay+15], fill=ANCHOR)
D.ellipse([ax-5,ay-5,ax+5,ay+5], fill=PAPER)
for t in range(0,360,20):
    rr=math.radians(t)
    D.line([(ax+math.cos(rr)*34,ay+math.sin(rr)*34),(ax+math.cos(rr)*48,ay+math.sin(rr)*48)],
           fill=ANCHOR, width=2)
D.text((ax+64, ay-14), "YOU ARE HERE", font=f_monoC, fill=INK2)
D.text((ax+64, ay+22), "本周你将抵达此处", font=f_zhS, fill=INK2)

# ---------- sunset disc (small, centered) ----------
scx, scy = W/2, 630
D.ellipse([scx-122, scy-122, scx+122, scy+122], fill=SUN)
D.ellipse([scx-140, scy-140, scx+140, scy+140], outline=SUN2, width=3)
D.ellipse([scx-156, scy-156, scx+156, scy+156], outline=(206,184,140), width=1)

# ---------- header marginalia ----------
D.text((ML, 92), "LAT 33.5941 N", font=f_monoD, fill=INK2)
D.text((ML, 138), "LON 118.1142 E", font=f_monoD, fill=INK2)
r1 = "SEA CHART · NO.2024"
r2 = "FRIDAY WEEKEND EDITION"
D.text((W-MR-tw(r1,f_monoD), 92), r1, font=f_monoD, fill=INK2)
D.text((W-MR-tw(r2,f_monoD), 138), r2, font=f_monoD, fill=INK2)
D.rectangle([ML, 216, W-MR, 219], fill=RUST)

# ---------- a single frame corner ticks (top edge only) ----------
tk=36
for (cx,cy,dx,dy) in [(ML,216,-1,1),(W-MR,216,1,1),(ML,2300,-1,1),(W-MR,2300,1,1)]:
    D.line([(cx+dx*tk,cy),(cx+dx*6,cy)], fill=INK, width=4)
    D.line([(cx,cy),(cx,cy+dy*6)], fill=INK, width=4)

# ---------- english italic poetic line ----------
sub="go see the sea"
D.text(((W-tw(sub,f_ser))/2, 250), sub, font=f_ser, fill=RUST)

# ---------- MAIN TITLE ----------
title="周五下班就去看海"
total=tracked_cx(D, W_CEN, 1090, title, f_tit, INK, tracking=80)

# ---------- sub2 ----------
sub2="THE WEEKEND BEGINS WHERE THE LAND ENDS"
sw=tw(sub2, f_monoB)
D.text(((W-sw)/2, 1500), sub2, font=f_monoB, fill=INK2)
rule=110
D.line([(W/2-sw/2-rule-26, 1524),(W/2-sw/2-26,1524)], fill=RUST, width=2)
D.line([(W/2+sw/2+26, 1524),(W/2+sw/2+rule+26,1524)], fill=RUST, width=2)

# ---------- tide gauge (right margin, sky->sea) ----------
tgx = W-MR-30
D.text((tgx-tw("TIDE",f_monoD), 1740), "TIDE", font=f_monoD, fill=INKSOFT)
for k in range(7):
    ty = 1800 + k*60
    D.line([(tgx, ty),(tgx+34, ty)], fill=INKSOFT, width=2)
D.line([(tgx, 1800),(tgx, 1800+6*60)], fill=INKSOFT, width=2)
D.text((tgx, 1800+6*60+12), "m", font=f_monoD, fill=INKSOFT)

# ---------- vertical marginal note (left) ----------
vx = 122
verts = ["向","海","而","行"]
vy = 1520
for ch in verts:
    D.text((vx, ay*0+1520) if False else (vx, vy), ch, font=f_song, fill=INKSOFT)
    vy += 92
D.line([(vx+8, vy+8),(vx+8, 2100)], fill=GRID, width=2)
rc="FRIDAY 17:00"
for k,ch in enumerate(rc):
    D.text((vx-6, vy+20+k*30), ch, font=f_monoD, fill=INKSOFT)

# ---------- wave-length reference in sky gap ----------
for yy,lbl in [(1820,"HW 2.1"),(1920,"MW 1.3"),(2020,"LW 0.4")]:
    D.line([(W/2-70, yy),(W/2+70, yy)], fill=(200,188,166), width=1)
    D.text((W/2+86, yy-16), lbl, font=f_monoD, fill=INKSOFT)

# ---------- RECRUIT INDEX card ----------
carY = H - 560
cardH = H - 92 - carY
D.rectangle([ML, carY, W-MR, H-92], fill=PAPER2, outline=(168,158,142), width=2)
CX0, CX1 = ML+64, W-MR-64          # inner content bounds
# card top title (left) + QR (right)
D.text((CX0, carY+52), "RECRUIT INDEX", font=f_monoB, fill=INK)
D.text((CX0+tw("RECRUIT INDEX",f_monoB)+26, carY+56),
       "／ 招聘索引", font=f_zhM, fill=INK2)
# separator (only under the title, leaving the QR corner free)
D.line([(CX0, carY+170),(CX0+1080, carY+170)], fill=RUST, width=2)

# ---------- QR (top-right corner) ----------
qr = 250; qrx = CX1 - qr; qry = carY + 66; cell = qr//11
D.rectangle([qrx-16, qry-16, qrx+qr+16, qry+qr+16], fill=PAPER2)   # quiet zone
D.rectangle([qrx, qry, qrx+qr, qry+qr], outline=INK, width=3)
def finder(ox, oy):
    for a in range(7):
        for b in range(7):
            if a==0 or a==6 or b==0 or b==6 or (2<=a<=4 and 2<=b<=4):
                D.rectangle([ox+a*cell, oy+b*cell, ox+a*cell+cell-2, oy+b*cell+cell-2], fill=INK)
finder(qrx, qry); finder(qrx+8*cell, qry); finder(qrx, qry+8*cell)
random.seed(21)
for gy in range(11):
    for gx in range(11):
        if (0<=gx<3 and 0<=gy<3) or (8<=gx<11 and 0<=gy<3) or (0<=gx<3 and 8<=gy<11):
            continue
        if random.random()<0.46:
            D.rectangle([qrx+gx*cell, qry+gy*cell, qrx+gx*cell+cell-2, qry+gy*cell+cell-2], fill=INK)
D.text((qrx, qry+qr+22), "扫码投递 · SCAN", font=f_zhS, fill=INK2, anchor="ls")

# ---------- two-column index body ----------
midX = CX0 + 1040                 # column divider x
D.line([(midX, carY+190),(midX, carY+330)], fill=(186,176,160), width=2)
colL, colR = CX0+10, midX+46

# left column: open roles
D.text((colL, carY+196), "OPEN ROLES", font=f_monoD, fill=INKSOFT)
roles=[("创意","品牌"),("界面","体验"),("工程","交互")]
for k,(a,b) in enumerate(roles):
    yy = carY+252+k*64
    D.text((colL, yy), f"0{k+1}", font=f_monoD, fill=RUST)
    D.text((colL+110, yy-6), a, font=f_zhM, fill=INK)
    D.text((colL+250, yy+4), "· "+b, font=f_zhS, fill=INK2)
    D.line([(colL, yy+40),(midX-10, yy+40)], fill=(206,197,181), width=1)

# right column: application
D.text((colR, carY+196), "APPLICATION", font=f_monoD, fill=INKSOFT)
info=[("投递","work@tidal.team"),
      ("电话","0592-400-1700"),
      ("备注","请注明「向海」")]
for k,(a,b) in enumerate(info):
    yy = carY+252+k*64
    D.text((colR, yy), a, font=f_zhS, fill=INKSOFT)
    D.text((colR+92, yy+4), b, font=f_monoD, fill=INK)

# ---------- bottom caption ----------
cap="FRIDAY · 17:00 · 向海而行 · GO SEE THE SEA"
D.text(((W-tw(cap,f_monoC))/2, carY+cardH-44), cap, font=f_monoC, fill=INKSOFT)

# bottom caption line
cap="FRIDAY · 5:00 PM · 向海而行 · GO SEE THE SEA"
D.text(((W-tw(cap,f_monoC))/2, carY+cardH-46), cap, font=f_monoC, fill=INKSOFT)

# ---------- outer frame ----------
D.rectangle([14,14,W-14,H-14], outline=INK, width=4)
D.rectangle([34,34,W-34,H-34], outline=RUST, width=2)

IMG.save("/Users/lisun/workplace/go-harness/web/sessions/session-31/workspace/poster_v2.png")
print("saved v2", IMG.size)
