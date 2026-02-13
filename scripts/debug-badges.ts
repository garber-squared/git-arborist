/**
 * Debug script to check badges in database
 * Run with: npx tsx scripts/debug-badges.ts
 */

import { createClient } from '@supabase/supabase-js';
import * as dotenv from 'dotenv';

dotenv.config({ path: '.env' });
dotenv.config({ path: '.env.local' });

const supabaseUrl = process.env.VITE_SUPABASE_URL || process.env.SUPABASE_URL;
const supabaseKey = process.env.SUPABASE_SERVICE_ROLE_KEY || process.env.VITE_SUPABASE_ANON_KEY;

if (!supabaseUrl || !supabaseKey) {
  console.error('Missing SUPABASE_URL or SUPABASE_SERVICE_ROLE_KEY');
  process.exit(1);
}

const supabase = createClient(supabaseUrl, supabaseKey);

async function debug() {
  console.log('\n🔍 Debugging Badge Tables\n');
  console.log('Supabase URL:', supabaseUrl);
  console.log('Using key type:', supabaseKey?.startsWith('eyJ') ? 'JWT token' : 'Service role key');

  // Check badges table directly
  console.log('\n📋 BADGES TABLE (all badges):');
  console.log('─'.repeat(60));

  const { data: badges, error: badgesError } = await supabase
    .from('badges')
    .select('id, name, slug, created_at')
    .order('created_at', { ascending: false })
    .limit(10);

  if (badgesError) {
    console.error('❌ Error fetching badges:', badgesError.message);
  } else {
    console.log(`Found ${badges?.length || 0} badges:\n`);
    badges?.forEach((b, i) => {
      console.log(`  ${i + 1}. ${b.name}`);
      console.log(`     slug: ${b.slug}`);
      console.log(`     id: ${b.id}`);
      console.log(`     created: ${b.created_at}`);
      console.log('');
    });
  }

  // Check badges_with_status view
  console.log('\n📊 BADGES_WITH_STATUS VIEW:');
  console.log('─'.repeat(60));

  const { data: badgesWithStatus, error: viewError } = await supabase
    .from('badges_with_status')
    .select('id, name, slug, computed_bucket, enrollment_status, engagement_status')
    .order('created_at', { ascending: false })
    .limit(10);

  if (viewError) {
    console.error('❌ Error fetching badges_with_status:', viewError.message);
    console.log('\n⚠️  The view might not exist or there may be a permissions issue.');
    console.log('   Check if migration 20260129125932_add_badge_timing_fields.sql has been applied.');
  } else {
    console.log(`Found ${badgesWithStatus?.length || 0} badges in view:\n`);
    badgesWithStatus?.forEach((b, i) => {
      console.log(`  ${i + 1}. ${b.name}`);
      console.log(`     slug: ${b.slug}`);
      console.log(`     bucket: ${b.computed_bucket}`);
      console.log(`     enrollment: ${b.enrollment_status}`);
      console.log(`     engagement: ${b.engagement_status}`);
      console.log('');
    });
  }

  // Check for "pie" badges specifically
  console.log('\n🥧 SEARCHING FOR "PIE" BADGES:');
  console.log('─'.repeat(60));

  const { data: pieBadges, error: pieError } = await supabase
    .from('badges')
    .select('*')
    .ilike('slug', '%pie%');

  if (pieError) {
    console.error('❌ Error:', pieError.message);
  } else if (pieBadges?.length === 0) {
    console.log('❌ No badges found with "pie" in slug');
  } else {
    console.log(`Found ${pieBadges?.length} pie badge(s):\n`);
    pieBadges?.forEach(b => {
      console.log(`  Name: ${b.name}`);
      console.log(`  Slug: ${b.slug}`);
      console.log(`  ID: ${b.id}`);
      console.log(`  enrollment_start_at: ${b.enrollment_start_at}`);
      console.log(`  enrollment_end_at: ${b.enrollment_end_at}`);
      console.log(`  engagement_start_at: ${b.engagement_start_at}`);
      console.log(`  engagement_end_at: ${b.engagement_end_at}`);
      console.log('');
    });
  }

  // Compare counts
  console.log('\n📈 COMPARISON:');
  console.log('─'.repeat(60));

  const { count: badgeCount } = await supabase
    .from('badges')
    .select('*', { count: 'exact', head: true });

  const { count: viewCount } = await supabase
    .from('badges_with_status')
    .select('*', { count: 'exact', head: true });

  console.log(`  badges table count: ${badgeCount}`);
  console.log(`  badges_with_status view count: ${viewCount}`);

  if (badgeCount !== viewCount) {
    console.log('\n⚠️  MISMATCH! Some badges are not appearing in the view.');
  } else {
    console.log('\n✅ Counts match - all badges appear in view.');
  }
}

debug().catch(console.error);
