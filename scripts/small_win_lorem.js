// Browser Console Script: Populate Small Win Form with Random Lorem Ipsum
// Paste this into DevTools Console (F12 → Console) on http://localhost:8080/dashboard/ideas
// Self-contained - no external dependencies required

(function populateSmallWinForm() {
  // Built-in lorem ipsum generator
  const loremWords = [
    'lorem', 'ipsum', 'dolor', 'sit', 'amet', 'consectetur', 'adipiscing', 'elit',
    'sed', 'do', 'eiusmod', 'tempor', 'incididunt', 'ut', 'labore', 'et', 'dolore',
    'magna', 'aliqua', 'enim', 'ad', 'minim', 'veniam', 'quis', 'nostrud',
    'exercitation', 'ullamco', 'laboris', 'nisi', 'aliquip', 'ex', 'ea', 'commodo',
    'consequat', 'duis', 'aute', 'irure', 'in', 'reprehenderit', 'voluptate',
    'velit', 'esse', 'cillum', 'fugiat', 'nulla', 'pariatur', 'excepteur', 'sint',
    'occaecat', 'cupidatat', 'non', 'proident', 'sunt', 'culpa', 'qui', 'officia',
    'deserunt', 'mollit', 'anim', 'id', 'est', 'laborum', 'perspiciatis', 'unde',
    'omnis', 'iste', 'natus', 'error', 'voluptatem', 'accusantium', 'doloremque',
    'laudantium', 'totam', 'rem', 'aperiam', 'eaque', 'ipsa', 'quae', 'ab', 'illo',
    'inventore', 'veritatis', 'quasi', 'architecto', 'beatae', 'vitae', 'dicta',
    'explicabo', 'nemo', 'ipsam', 'quia', 'voluptas', 'aspernatur', 'aut', 'odit',
    'fugit', 'consequuntur', 'magni', 'dolores', 'eos', 'ratione', 'sequi',
    'nesciunt', 'neque', 'porro', 'quisquam', 'dolorem', 'adipisci', 'numquam',
    'eius', 'modi', 'tempora', 'magnam', 'quaerat'
  ];

  function randomInt(min, max) {
    return Math.floor(Math.random() * (max - min + 1)) + min;
  }

  function pickRandom(arr, count) {
    const shuffled = [...arr].sort(() => 0.5 - Math.random());
    return shuffled.slice(0, count);
  }

  function generateWords(count) {
    return pickRandom(loremWords, count).join(' ');
  }

  function generateSentence() {
    const wordCount = randomInt(6, 14);
    const sentence = generateWords(wordCount);
    return sentence.charAt(0).toUpperCase() + sentence.slice(1) + '.';
  }

  function generateSentences(count) {
    return Array.from({ length: count }, () => generateSentence()).join(' ');
  }

  function generateParagraph() {
    const sentenceCount = randomInt(3, 5);
    return generateSentences(sentenceCount);
  }

  function generateTitle() {
    const words = generateWords(randomInt(3, 6));
    return words.charAt(0).toUpperCase() + words.slice(1);
  }

  // Helper to set React input value (triggers onChange properly)
  function setReactInputValue(element, value) {
    let setter;
    if (element instanceof HTMLTextAreaElement) {
      setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
    } else if (element instanceof HTMLInputElement) {
      setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    }

    if (setter) {
      setter.call(element, value);
      element.dispatchEvent(new Event('input', { bubbles: true }));
    }
  }

  // Helper to simulate typing a tag and pressing Enter
  function addTag(tagInputElement, tagText) {
    setReactInputValue(tagInputElement, tagText);
    tagInputElement.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter',
      code: 'Enter',
      keyCode: 13,
      which: 13,
      bubbles: true
    }));
  }

  // Pool of realistic headlines
  const headlinePool = [
    'Breakthrough moment in student engagement',
    'Finally cracked the participation problem',
    'Small group work paid off today',
    'A quiet student found their voice',
    'Parent conference went better than expected',
    'New seating arrangement is working',
    'Morning routine finally clicked',
    'Differentiation strategy success',
    'Technology integration win',
    'Classroom management milestone',
    'Student-led discussion worked beautifully',
    'Formative assessment insight',
    'Co-teaching collaboration breakthrough',
    'Behavior intervention success',
    'Reading group made progress',
    'Math manipulatives clicked for struggling learner',
    'Project-based learning momentum',
    'SEL check-in revealed important insight',
    'Peer tutoring exceeded expectations',
    'Flexible grouping experiment worked',
    'Exit ticket data was encouraging',
    'Student took ownership of learning',
    'Restorative conversation made impact',
    'Family engagement success story',
    'Scaffolding strategy paid off'
  ];

  // Pool of realistic goals
  const goalPool = [
    'Improve classroom participation',
    'Build student confidence',
    'Increase family engagement',
    'Strengthen classroom community',
    'Support struggling readers',
    'Develop student leadership',
    'Enhance formative assessment practices',
    'Create more inclusive environment',
    'Improve transition times',
    'Build growth mindset culture',
    'Increase student voice and choice',
    'Strengthen peer relationships',
    'Support social-emotional learning',
    'Improve differentiated instruction',
    'Enhance collaborative learning',
    'Build student agency',
    'Strengthen home-school connection',
    'Develop self-regulation skills',
    'Increase academic engagement',
    'Foster inquiry-based learning'
  ];

  // Pool of realistic educational tags to pick from
  const tagPool = [
    'student voice', 'engagement', 'participation', 'classroom culture',
    'differentiation', 'family partnership', 'formative assessment',
    'peer collaboration', 'growth mindset', 'student agency',
    'inclusive practices', 'literacy', 'numeracy', 'social-emotional',
    'feedback', 'scaffolding', 'inquiry-based', 'project-based',
    'culturally responsive', 'restorative practices'
  ];

  // Generate random content from pools
  const loremTitle = headlinePool[randomInt(0, headlinePool.length - 1)];
  const loremGoal = goalPool[randomInt(0, goalPool.length - 1)];

  // Generate a structured story with the form's prompt pattern
  const loremStory = `What I did or tried was ${generateSentences(2).toLowerCase().slice(0, -1)}.

What happened next was ${generateSentences(2).toLowerCase().slice(0, -1)}.

My takeaway is ${generateSentences(2).toLowerCase()}`;

  // Pick 3-5 random tags
  const tagCount = randomInt(3, 5);
  const dummyTags = pickRandom(tagPool, tagCount);

  // Randomly select win type
  const winTypes = ['Success', 'Lesson Learned'];
  const selectedType = winTypes[randomInt(0, 1)];

  // 1. Click the selected type button
  const typeButtons = document.querySelectorAll('button[type="button"]');
  const targetButton = Array.from(typeButtons).find(btn =>
    btn.textContent?.includes(selectedType)
  );
  if (targetButton) {
    targetButton.click();
    console.log(`✓ Selected "${selectedType}" type`);
  }

  // 2. Fill in Headline
  const headlineInput = document.getElementById('reflection-title');
  if (headlineInput) {
    setReactInputValue(headlineInput, loremTitle);
    console.log(`✓ Filled headline: "${loremTitle}"`);
  }

  // 3. Fill in Related Goal
  const goalInput = document.getElementById('reflection-goal');
  if (goalInput) {
    setReactInputValue(goalInput, loremGoal);
    console.log(`✓ Filled related goal: "${loremGoal}"`);
  }

  // 4. Fill in Story
  const storyTextarea = document.getElementById('reflection-story');
  if (storyTextarea) {
    setReactInputValue(storyTextarea, loremStory);
    console.log('✓ Filled story with lorem ipsum');
  }

  // 5. Add tags (with slight delays to allow React state updates)
  const tagsInput = document.getElementById('reflection-tags');
  if (tagsInput) {
    dummyTags.forEach((tag, index) => {
      setTimeout(() => {
        addTag(tagsInput, tag);
        console.log(`✓ Added tag: "${tag}"`);

        // Log completion message after last tag
        if (index === dummyTags.length - 1) {
          console.log('\n🎉 Form populated with random lorem ipsum! Review and click "Save Story" when ready.');
        }
      }, index * 100);
    });
  } else {
    console.log('\n🎉 Form populated! Review and click "Save Story" when ready.');
  }
})();
