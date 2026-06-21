#!/usr/bin/env python3
"""Render the Supervisor Operations Manual markdown into a branded, page-numbered PDF.

DEV TOOL — NOT run in CI. Regenerates the static asset served from the landing
page (apps/web/public/supervisor-operations-manual.pdf) from the canonical
source (docs/supervisor-operations-manual.md).

How to regenerate
-----------------
This script embeds Arial (full Unicode coverage: em-dashes, arrows, curly
quotes, bullets). The font directory default is platform-aware (Windows
C:\\Windows\\Fonts, macOS /Library/Fonts, Linux msttcorefonts). To point it at a
different directory holding arial.ttf / arialbd.ttf / ariali.ttf / arialbi.ttf
(or substitute another family), set the MANUAL_FONT_DIR environment variable.

    pip install --quiet reportlab pypdf
    python scripts/build-supervisor-manual-pdf.py \
        docs/supervisor-operations-manual.md \
        apps/web/public/supervisor-operations-manual.pdf

Then commit the regenerated apps/web/public/supervisor-operations-manual.pdf
binary (explicit path; never `git add -A`).
"""
import re, sys, datetime
from reportlab.lib.pagesizes import A4
from reportlab.lib.units import mm
from reportlab.lib import colors
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from reportlab.lib.enums import TA_LEFT, TA_CENTER
from reportlab.platypus import (SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle,
                                HRFlowable, PageBreak, ListFlowable, ListItem, KeepTogether)
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
import os

SRC = sys.argv[1]
OUT = sys.argv[2]

# Embed Arial (full Unicode coverage: em-dashes, arrows, curly quotes, bullets).
# Font directory default is platform-aware; override with MANUAL_FONT_DIR to
# point at any directory holding arial.ttf / arialbd.ttf / ariali.ttf /
# arialbi.ttf (or substitute another family).
def _default_font_dir():
    if sys.platform.startswith("win"):
        return r"C:\Windows\Fonts"
    if sys.platform == "darwin":
        return "/Library/Fonts"
    return "/usr/share/fonts/truetype/msttcorefonts"

_FD = os.environ.get("MANUAL_FONT_DIR", _default_font_dir())
pdfmetrics.registerFont(TTFont("AppFont", os.path.join(_FD, "arial.ttf")))
pdfmetrics.registerFont(TTFont("AppFont-Bold", os.path.join(_FD, "arialbd.ttf")))
pdfmetrics.registerFont(TTFont("AppFont-Italic", os.path.join(_FD, "ariali.ttf")))
pdfmetrics.registerFont(TTFont("AppFont-BoldItalic", os.path.join(_FD, "arialbi.ttf")))
pdfmetrics.registerFontFamily("AppFont", normal="AppFont", bold="AppFont-Bold",
                              italic="AppFont-Italic", boldItalic="AppFont-BoldItalic")
FN, FNB, FNI = "AppFont", "AppFont-Bold", "AppFont-Italic"

BRAND   = colors.HexColor("#0E5A6B")   # deep teal
ACCENT  = colors.HexColor("#2E8B8B")
INK     = colors.HexColor("#1C2B33")
MUTE    = colors.HexColor("#5B6B73")
LIGHT   = colors.HexColor("#EAF1F3")
CODECOL = colors.HexColor("#0B6E7A")
TODAY   = datetime.date.today().strftime("%d %B %Y")

ss = getSampleStyleSheet()
def style(name, **kw):
    base = kw.pop("parent", ss["Normal"])
    return ParagraphStyle(name, parent=base, **kw)

S = {
  "h1": style("h1", fontName=FNB, fontSize=17, textColor=BRAND, spaceBefore=16, spaceAfter=6, leading=21),
  "h2": style("h2", fontName=FNB, fontSize=13.5, textColor=BRAND, spaceBefore=14, spaceAfter=4, leading=17),
  "h3": style("h3", fontName=FNB, fontSize=11.5, textColor=ACCENT, spaceBefore=10, spaceAfter=3, leading=15),
  "h4": style("h4", fontName=FNB, fontSize=10.5, textColor=INK, spaceBefore=8, spaceAfter=2, leading=14),
  "body": style("body", fontName=FN, fontSize=10, textColor=INK, leading=15, spaceAfter=6, alignment=TA_LEFT),
  "bullet": style("bullet", fontName=FN, fontSize=10, textColor=INK, leading=14.5),
  "quote": style("quote", fontName=FNI, fontSize=10, textColor=MUTE, leading=15,
                 leftIndent=8, borderColor=ACCENT, borderWidth=0, backColor=LIGHT, spaceBefore=4, spaceAfter=6,
                 borderPadding=(6,6,6,8)),
  "cell": style("cell", fontName=FN, fontSize=9, textColor=INK, leading=12.5),
  "cellh": style("cellh", fontName=FNB, fontSize=9, textColor=colors.white, leading=12.5),
  "cover_t": style("cover_t", fontName=FNB, fontSize=30, textColor=BRAND, leading=36, alignment=TA_LEFT, spaceAfter=6),
  "cover_s": style("cover_s", fontName=FN, fontSize=13, textColor=MUTE, leading=18, alignment=TA_LEFT),
  "cover_b": style("cover_b", fontName=FNB, fontSize=12, textColor=ACCENT, leading=16, alignment=TA_LEFT),
}

def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

def inline(text):
    parts = re.split(r"(`[^`]+`)", text)
    out = []
    for p in parts:
        if len(p) >= 2 and p.startswith("`") and p.endswith("`"):
            out.append('<font face="Courier" color="#0B6E7A">' + esc(p[1:-1]) + "</font>")
        else:
            s = esc(p)
            s = re.sub(r"\*\*(.+?)\*\*", r"<b>\1</b>", s)
            s = re.sub(r"(?<!\*)\*(?!\s)(.+?)(?<!\s)\*(?!\*)", r"<i>\1</i>", s)
            s = re.sub(r"(?<![A-Za-z0-9])_(.+?)_(?![A-Za-z0-9])", r"<i>\1</i>", s)
            out.append(s)
    return "".join(out)

def build_table(rows):
    header, body = rows[0], rows[1:]
    data = [[Paragraph(inline(c), S["cellh"]) for c in header]]
    for r in body:
        data.append([Paragraph(inline(c), S["cell"]) for c in r])
    ncol = len(header)
    avail = A4[0] - 2*18*mm
    t = Table(data, colWidths=[avail/ncol]*ncol, repeatRows=1)
    ts = [("BACKGROUND",(0,0),(-1,0),BRAND),
          ("VALIGN",(0,0),(-1,-1),"TOP"),
          ("LINEBELOW",(0,0),(-1,0),0.6,BRAND),
          ("GRID",(0,0),(-1,-1),0.4,colors.HexColor("#C7D6DA")),
          ("LEFTPADDING",(0,0),(-1,-1),6),("RIGHTPADDING",(0,0),(-1,-1),6),
          ("TOPPADDING",(0,0),(-1,-1),4),("BOTTOMPADDING",(0,0),(-1,-1),4)]
    for i in range(1, len(data)):
        if i % 2 == 0:
            ts.append(("BACKGROUND",(0,i),(-1,i),colors.HexColor("#F4F8F9")))
    t.setStyle(TableStyle(ts))
    return t

def parse(md):
    lines = md.split("\n")
    flow = []
    i = 0
    first_h1_seen = False
    while i < len(lines):
        ln = lines[i]
        raw = ln.rstrip("\n")
        s = raw.strip()
        # table block
        if s.startswith("|") and "|" in s[1:]:
            block = []
            while i < len(lines) and lines[i].strip().startswith("|"):
                block.append(lines[i].strip())
                i += 1
            rows = []
            for b in block:
                if re.match(r"^\|[\s:|-]+\|?$", b):  # separator row
                    continue
                cells = [c.strip() for c in b.strip().strip("|").split("|")]
                rows.append(cells)
            if rows:
                flow.append(Spacer(1,2)); flow.append(build_table(rows)); flow.append(Spacer(1,6))
            continue
        # headings
        m = re.match(r"^(#{1,4})\s+(.*)$", s)
        if m:
            level = len(m.group(1)); txt = m.group(2)
            if level == 1:
                if not first_h1_seen:
                    first_h1_seen = True; i += 1; continue  # title used on cover
                flow.append(Paragraph(inline(txt), S["h1"]))
            elif level == 2:
                flow.append(Spacer(1,2))
                flow.append(Paragraph(inline(txt), S["h2"]))
                flow.append(HRFlowable(width="100%", thickness=0.8, color=ACCENT, spaceBefore=1, spaceAfter=5))
            elif level == 3:
                flow.append(Paragraph(inline(txt), S["h3"]))
            else:
                flow.append(Paragraph(inline(txt), S["h4"]))
            i += 1; continue
        # horizontal rule
        if re.match(r"^(-{3,}|\*{3,}|_{3,})$", s):
            flow.append(HRFlowable(width="100%", thickness=0.5, color=colors.HexColor("#C7D6DA"), spaceBefore=6, spaceAfter=6))
            i += 1; continue
        # blockquote
        if s.startswith(">"):
            buf = []
            while i < len(lines) and lines[i].strip().startswith(">"):
                buf.append(lines[i].strip()[1:].strip()); i += 1
            flow.append(Paragraph(inline(" ".join(buf)), S["quote"]))
            continue
        # lists (bullet or numbered) as hanging-indent paragraphs (robust)
        if re.match(r"^(\s*)([-*]|\d+\.)\s+", ln):
            while i < len(lines) and re.match(r"^(\s*)([-*]|\d+\.)\s+", lines[i]):
                lm = re.match(r"^(\s*)([-*]|(\d+)\.)\s+(.*)$", lines[i])
                indent = len(lm.group(1)); num = lm.group(3); txt = lm.group(4)
                left = 8 + (indent // 2) * 16
                pref = (num + ". ") if num else "•  "
                st = ParagraphStyle("li", parent=S["bullet"], leftIndent=left+15,
                                    firstLineIndent=-15, spaceAfter=2)
                flow.append(Paragraph('<font color="#2E8B8B"><b>' + pref + "</b></font>" + inline(txt), st))
                i += 1
            flow.append(Spacer(1,4))
            continue
        # blank
        if s == "":
            i += 1; continue
        # paragraph (gather until blank / structural)
        buf = [raw]
        i += 1
        while i < len(lines):
            nx = lines[i].strip()
            if nx == "" or nx.startswith("#") or nx.startswith("|") or nx.startswith(">") \
               or re.match(r"^(\s*)([-*]|\d+\.)\s+", lines[i]) or re.match(r"^(-{3,}|\*{3,}|_{3,})$", nx):
                break
            buf.append(lines[i].rstrip()); i += 1
        flow.append(Paragraph(inline(" ".join(buf)), S["body"]))
    return flow

def header_footer(canvas, doc):
    canvas.saveState()
    w, h = A4
    # footer line
    canvas.setStrokeColor(colors.HexColor("#C7D6DA")); canvas.setLineWidth(0.5)
    canvas.line(18*mm, 14*mm, w-18*mm, 14*mm)
    canvas.setFont(FN, 8); canvas.setFillColor(MUTE)
    canvas.drawString(18*mm, 9*mm, "FuelGrid OS — Supervisor Operations Manual")
    canvas.drawRightString(w-18*mm, 9*mm, "Page %d" % doc.page)
    # top brand band (skinny) on content pages
    canvas.setFillColor(BRAND)
    canvas.rect(0, h-6*mm, w, 6*mm, stroke=0, fill=1)
    canvas.restoreState()

def cover(canvas, doc):
    w, h = A4
    canvas.saveState()
    canvas.setFillColor(BRAND); canvas.rect(0, h-70*mm, w, 70*mm, stroke=0, fill=1)
    canvas.setFillColor(ACCENT); canvas.rect(0, h-72*mm, w, 2*mm, stroke=0, fill=1)
    canvas.setFillColor(colors.white)
    canvas.setFont(FNB, 13); canvas.drawString(18*mm, h-22*mm, "FUELGRID OS")
    canvas.setFont(FN, 9); canvas.drawString(18*mm, h-28*mm, "Fuel-station operating system")
    canvas.setFont(FNB, 30); canvas.drawString(18*mm, h-50*mm, "Supervisor")
    canvas.drawString(18*mm, h-62*mm, "Operations Manual")
    canvas.setFillColor(MUTE); canvas.setFont(FN, 11)
    canvas.drawString(18*mm, h-90*mm, "A simple, step-by-step guide to running shifts with the")
    canvas.drawString(18*mm, h-96*mm, "main app (computer) and the attendant mobile app (phone).")
    canvas.setFillColor(INK); canvas.setFont(FNB, 10)
    canvas.drawString(18*mm, h-112*mm, "Written for everyone — no computer experience needed.")
    canvas.setStrokeColor(ACCENT); canvas.setLineWidth(1)
    canvas.line(18*mm, 30*mm, 80*mm, 30*mm)
    canvas.setFillColor(MUTE); canvas.setFont(FN, 9)
    canvas.drawString(18*mm, 23*mm, "Generated %s" % TODAY)
    canvas.drawString(18*mm, 18*mm, "Keep this guide updated as the app changes.")
    canvas.restoreState()

def main():
    md = open(SRC, encoding="utf-8").read()
    story = parse(md)
    doc = SimpleDocTemplate(OUT, pagesize=A4,
                            leftMargin=18*mm, rightMargin=18*mm,
                            topMargin=16*mm, bottomMargin=18*mm,
                            title="FuelGrid OS - Supervisor Operations Manual",
                            author="FuelGrid OS")
    # cover page is drawn by onFirstPage; push content to start on page 2
    front = [Spacer(1, 1), PageBreak()] + story
    doc.build(front, onFirstPage=cover, onLaterPages=header_footer)
    print("wrote", OUT)

main()
