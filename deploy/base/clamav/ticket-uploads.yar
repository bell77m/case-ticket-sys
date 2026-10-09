// YARA rules for guest evidence uploads (NFR-5), loaded by clamd (clamd.conf in this folder). Every upload has already
// passed the magic-byte check (JPEG, PNG, HEIC, MP4, MOV), so these look for what has no place inside a photo or video.
// ClamAV runs plain strings and conditions, not YARA modules (pe, elf, ...). Strings stay 6 bytes or longer, so random
// bytes in a 100 MB video do not match by chance; test a new rule against real phone photos and videos first.
// They catch the obvious payloads only: short markers such as "<?=" or an appended ZIP would match random video
// bytes too often. Files are also served with their exact type, nosniff and a sandbox CSP, so a miss cannot run.

// The EICAR test string: proves the whole chain works (make test, e2e, and by hand on a new server).
rule Ticket_EICAR_Test
{
	strings:
		$eicar = "X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
	condition:
		$eicar
}

// A program hidden in the file: Windows PE, Linux ELF, or an OLE container (old Office documents, MSI).
rule Ticket_Binary_In_Media
{
	strings:
		$pe = "This program cannot be run in DOS mode"
		$elf32 = { 7F 45 4C 46 01 01 01 00 00 00 00 00 00 00 00 00 }
		$elf64 = { 7F 45 4C 46 02 01 01 00 00 00 00 00 00 00 00 00 }
		$ole = { D0 CF 11 E0 A1 B1 1A E1 }
	condition:
		any of them
}

// Script or markup that a server, shell or browser would run (web shells and polyglots hidden in image data).
rule Ticket_Script_In_Media
{
	strings:
		$php_sp = "<?php " nocase
		$php_nl = "<?php\n" nocase
		$php_cr = "<?php\x0d" nocase // ClamAV's YARA parser has no \r
		$php_tab = "<?php\t" nocase
		$script = "<script" nocase
		$iframe = "<iframe" nocase
		$js_uri = "javascript:" nocase
		$sh = "#!/bin/"
		$env = "#!/usr/bin/env "
		$ps = "powershell" nocase
		$b64 = "base64_decode(" nocase
		$jsp = "<%@ page" nocase
	condition:
		any of them
}
